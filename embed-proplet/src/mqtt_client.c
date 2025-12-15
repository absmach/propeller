// #include <zephyr/data/json.h>
#include "mqtt_client.h"
#include "cJSON.h"
#include "net/mqtt.h"
#include "wasm_handler.h"

#include <errno.h>
#include <stdlib.h>
#include <string.h>

#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>
#include <zephyr/net/socket.h>
#include <zephyr/random/random.h>
#include <zephyr/sys/base64.h>

#if defined(CONFIG_CPU_LOAD)
#include <zephyr/debug/cpu_load.h>
#endif

#if defined(CONFIG_SYS_HEAP_RUNTIME_STATS)
#include <zephyr/sys/sys_heap.h>
#endif

LOG_MODULE_REGISTER(mqtt_client);

#define RX_BUFFER_SIZE 256
#define TX_BUFFER_SIZE 256

#define MQTT_BROKER_HOSTNAME "10.42.0.1" /* Replace with your broker's IP */
#define MQTT_BROKER_PORT 1883

#define REGISTRY_ACK_TOPIC_TEMPLATE "m/%s/c/%s/control/manager/registry"
#define ALIVE_TOPIC_TEMPLATE "m/%s/c/%s/control/proplet/alive"
#define DISCOVERY_TOPIC_TEMPLATE "m/%s/c/%s/control/proplet/create"
#define START_TOPIC_TEMPLATE "m/%s/c/%s/control/manager/start"
#define STOP_TOPIC_TEMPLATE "m/%s/c/%s/control/manager/stop"
#define REGISTRY_RESPONSE_TOPIC "m/%s/c/%s/registry/server"
#define FETCH_REQUEST_TOPIC_TEMPLATE "m/%s/c/%s/registry/proplet"
#define RESULTS_TOPIC_TEMPLATE "m/%s/c/%s/control/proplet/results"

#define METRICS_TOPIC_TEMPLATE "m/%s/c/%s/control/proplet/metrics"

#define WILL_MESSAGE_TEMPLATE \
  "{\"status\":\"offline\",\"proplet_id\":\"%s\",\"namespace\":\"%s\"}"
#define WILL_QOS MQTT_QOS_1_AT_LEAST_ONCE
#define WILL_RETAIN 1

#define CLIENT_ID "proplet-esp32s3"

#define DEFAULT_NAMESPACE "embedded"

#define MAX_ID_LEN 64
#define MAX_NAME_LEN 64
#define MAX_STATE_LEN 16
#define MAX_URL_LEN 256
#define MAX_TIMESTAMP_LEN 32
#define MAX_BASE64_LEN 1024
#define MAX_INPUTS 16
#define MAX_RESULTS 16

/* -------------------------------------------------------------------------- */
/* FIX #1: struct task must be defined before g_current_task                   */
/* -------------------------------------------------------------------------- */

struct task {
  char id[MAX_ID_LEN];
  char name[MAX_NAME_LEN];
  char state[MAX_STATE_LEN];
  char image_url[MAX_URL_LEN];

  char file[MAX_BASE64_LEN];
  size_t file_len;

  uint64_t inputs[MAX_INPUTS];
  size_t inputs_count;
  uint64_t results[MAX_RESULTS];
  size_t results_count;

  char start_time[MAX_TIMESTAMP_LEN];
  char finish_time[MAX_TIMESTAMP_LEN];
  char created_at[MAX_TIMESTAMP_LEN];
  char updated_at[MAX_TIMESTAMP_LEN];
};

/*
 * Keep the most recent "start" Task here, so if
 * we fetch the WASM from the registry, we can call
 * the WASM with the same inputs.
 *
 * If you support multiple tasks in parallel, you'll need
 * a more robust approach than a single global.
 */
static struct task g_current_task;

/* Store the connected proplet_id so alive can use it. */
static char g_proplet_id[MAX_ID_LEN];
static const char *g_namespace = DEFAULT_NAMESPACE;

static uint8_t rx_buffer[RX_BUFFER_SIZE];
static uint8_t tx_buffer[TX_BUFFER_SIZE];

static struct mqtt_client client_ctx;
static struct sockaddr_storage broker_addr;

static struct zsock_pollfd fds[1];
static int nfds;

bool mqtt_connected = false;

struct registry_response {
  char app_name[64];
  char data[MAX_BASE64_LEN];
};

static void prepare_fds(struct mqtt_client *client)
{
  if (client->transport.type == MQTT_TRANSPORT_NON_SECURE) {
    fds[0].fd = client->transport.tcp.sock;
  }
#if defined(CONFIG_MQTT_LIB_TLS)
  else if (client->transport.type == MQTT_TRANSPORT_SECURE) {
    fds[0].fd = client->transport.tls.sock;
  }
#endif
  fds[0].events = ZSOCK_POLLIN;
  nfds = 1;
}

static void clear_fds(void) { nfds = 0; }

static int poll_mqtt_socket(struct mqtt_client *client, int timeout)
{
  prepare_fds(client);
  if (nfds <= 0) {
    return -EINVAL;
  }

  int rc = zsock_poll(fds, nfds, timeout);
  if (rc < 0) {
    LOG_ERR("Socket poll error [%d]", rc);
  }
  return rc;
}

static void mqtt_event_handler(struct mqtt_client *client,
                               const struct mqtt_evt *evt)
{
  switch (evt->type) {
  case MQTT_EVT_CONNACK:
    if (evt->result != 0) {
      LOG_ERR("MQTT connection failed [%d]", evt->result);
    } else {
      mqtt_connected = true;
      LOG_INF("MQTT connection accepted by broker");
    }
    break;

  case MQTT_EVT_DISCONNECT:
    mqtt_connected = false;
    clear_fds();
    LOG_INF("Disconnected from MQTT broker");
    break;

  case MQTT_EVT_PUBLISH: {
    const struct mqtt_publish_param *pub = &evt->param.publish;
    char payload[PAYLOAD_BUFFER_SIZE];
    int ret;

    extern const char *channel_id;
    extern const char *domain_id;

    char start_topic[128];
    char stop_topic[128];
    char registry_response_topic[128];

    snprintf(start_topic, sizeof(start_topic), START_TOPIC_TEMPLATE,
             domain_id, channel_id);
    snprintf(stop_topic, sizeof(stop_topic), STOP_TOPIC_TEMPLATE,
             domain_id, channel_id);
    snprintf(registry_response_topic, sizeof(registry_response_topic),
             REGISTRY_RESPONSE_TOPIC, domain_id, channel_id);

    LOG_INF("Message received on topic: %s", pub->message.topic.topic.utf8);

    ret = mqtt_read_publish_payload(
        &client_ctx, payload,
        MIN(pub->message.payload.len, PAYLOAD_BUFFER_SIZE - 1));
    if (ret < 0) {
      LOG_ERR("Failed to read payload [%d]", ret);
      return;
    }
    payload[ret] = '\0'; /* Null-terminate */
    LOG_INF("Payload: %s", payload);

    if (strncmp(pub->message.topic.topic.utf8, start_topic,
                pub->message.topic.topic.size) == 0) {
      handle_start_command(payload);
    } else if (strncmp(pub->message.topic.topic.utf8, stop_topic,
                       pub->message.topic.topic.size) == 0) {
      handle_stop_command(payload);
    } else if (strncmp(pub->message.topic.topic.utf8, registry_response_topic,
                       pub->message.topic.topic.size) == 0) {
      (void)handle_registry_response(payload);
    } else {
      LOG_WRN("Unknown topic");
    }
    break;
  }

  case MQTT_EVT_SUBACK:
    LOG_INF("Subscribed successfully");
    break;

  case MQTT_EVT_PUBACK:
    LOG_INF("QoS 1 Message published successfully");
    break;

  case MQTT_EVT_PUBREC:
    LOG_INF("QoS 2 publish received");
    break;
  case MQTT_EVT_PUBREL:
    LOG_INF("QoS 2 publish released");
    break;
  case MQTT_EVT_PUBCOMP:
    LOG_INF("QoS 2 publish complete");
    break;
  case MQTT_EVT_UNSUBACK:
    LOG_INF("Unsubscribed successfully");
    break;
  case MQTT_EVT_PINGRESP:
    LOG_INF("Ping response received from broker");
    break;

  default:
    LOG_WRN("Unhandled MQTT event [%d]", evt->type);
    break;
  }
}

static void prepare_publish_param(struct mqtt_publish_param *param,
                                  const char *topic_str,
                                  const char *payload)
{
  memset(param, 0, sizeof(*param));

  param->message.topic.topic.utf8 = topic_str;
  param->message.topic.topic.size = strlen(topic_str);
  param->message.topic.qos = MQTT_QOS_1_AT_LEAST_ONCE;

  param->message.payload.data = (uint8_t *)payload;
  param->message.payload.len = strlen(payload);

  param->message_id = sys_rand32_get() & 0xFFFF;
  param->dup_flag = 0;
  param->retain_flag = 0;
}

int publish(const char *domain_id, const char *channel_id,
            const char *topic_template, const char *payload)
{
  if (!mqtt_connected) {
    LOG_ERR("MQTT client is not connected. Cannot publish.");
    return -ENOTCONN;
  }

  char topic[128];
  snprintf(topic, sizeof(topic), topic_template, domain_id, channel_id);

  struct mqtt_publish_param param;
  prepare_publish_param(&param, topic, payload);

  int ret = mqtt_publish(&client_ctx, &param);
  if (ret != 0) {
    LOG_ERR("Failed to publish to topic: %s. Error: %d", topic, ret);
    return ret;
  }

  LOG_INF("Published to topic: %s. Payload: %s", topic, payload);
  return 0;
}

/* -------------------------------------------------------------------------- */
/* FIX #2/#3: connect uses passed proplet_id (no hardcoded placeholders)       */
/*             and stores it for alive publishing.                            */
/* NOTE: password is intentionally unset here because your header has 3 args. */
/* -------------------------------------------------------------------------- */
int mqtt_client_connect(const char *domain_id,
                        const char *proplet_id,
                        const char *channel_id)
{
  int ret;
  struct sockaddr_in *broker = (struct sockaddr_in *)&broker_addr;

  broker->sin_family = AF_INET;
  broker->sin_port = htons(MQTT_BROKER_PORT);

  ret = net_addr_pton(AF_INET, MQTT_BROKER_HOSTNAME, &broker->sin_addr);
  if (ret != 0) {
    LOG_ERR("Failed to resolve broker address, ret=%d", ret);
    return ret;
  }

  mqtt_client_init(&client_ctx);

  /* Save proplet_id for alive messages. */
  strncpy(g_proplet_id, proplet_id, sizeof(g_proplet_id) - 1);
  g_proplet_id[sizeof(g_proplet_id) - 1] = '\0';

  char will_topic_str[128];
  snprintf(will_topic_str, sizeof(will_topic_str),
           ALIVE_TOPIC_TEMPLATE, domain_id, channel_id);

  char will_message_str[256];
  snprintf(will_message_str, sizeof(will_message_str),
           WILL_MESSAGE_TEMPLATE, proplet_id, g_namespace);

  struct mqtt_utf8 will_message = {
      .utf8 = (const uint8_t *)will_message_str,
      .size = strlen(will_message_str),
  };

  struct mqtt_topic will_topic = {
      .topic =
          {
              .utf8 = (const uint8_t *)will_topic_str,
              .size = strlen(will_topic_str),
          },
      .qos = WILL_QOS,
  };

  client_ctx.broker = &broker_addr;
  client_ctx.evt_cb = mqtt_event_handler;
  client_ctx.client_id = MQTT_UTF8_LITERAL(CLIENT_ID);

  /* Username = proplet_id passed from main.c */
  static struct mqtt_utf8 username;
  username.utf8 = (const uint8_t *)g_proplet_id;
  username.size = strlen(g_proplet_id);
  client_ctx.user_name = &username;

  /* No password in 3-arg API. Leave unset unless your broker requires it. */
  client_ctx.password = NULL;

  client_ctx.protocol_version = MQTT_VERSION_3_1_1;

  client_ctx.rx_buf = rx_buffer;
  client_ctx.rx_buf_size = RX_BUFFER_SIZE;
  client_ctx.tx_buf = tx_buffer;
  client_ctx.tx_buf_size = TX_BUFFER_SIZE;

  /* If you enable will later, these are the correct fields:
   * client_ctx.will_topic = &will_topic;
   * client_ctx.will_message = &will_message;
   * client_ctx.will_retain = WILL_RETAIN;
   */

  while (!mqtt_connected) {
    LOG_INF("Attempting to connect to the MQTT broker...");

    ret = mqtt_connect(&client_ctx);
    if (ret != 0) {
      LOG_ERR("MQTT connect failed [%d]. Retrying in 5 seconds...", ret);
      k_sleep(K_SECONDS(5));
      continue;
    }

    ret = poll_mqtt_socket(&client_ctx, 5000);
    if (ret < 0) {
      LOG_ERR("Socket poll failed, ret=%d. Retrying in 5 seconds...", ret);
      mqtt_abort(&client_ctx);
      k_sleep(K_SECONDS(5));
      continue;
    } else if (ret == 0) {
      LOG_ERR("Poll timed out waiting for CONNACK. Retrying in 5 seconds...");
      mqtt_abort(&client_ctx);
      k_sleep(K_SECONDS(5));
      continue;
    }

    mqtt_input(&client_ctx);

    if (!mqtt_connected) {
      LOG_ERR("MQTT connection not established. Retrying...");
      mqtt_abort(&client_ctx);
    }
  }

  LOG_INF("MQTT client connected successfully");
  return 0;
}

int subscribe(const char *domain_id, const char *channel_id)
{
  char start_topic[128];
  char stop_topic[128];
  char registry_response_topic[128];

  snprintf(start_topic, sizeof(start_topic),
           START_TOPIC_TEMPLATE, domain_id, channel_id);
  snprintf(stop_topic, sizeof(stop_topic),
           STOP_TOPIC_TEMPLATE, domain_id, channel_id);
  snprintf(registry_response_topic, sizeof(registry_response_topic),
           REGISTRY_RESPONSE_TOPIC, domain_id, channel_id);

  struct mqtt_topic topics[] = {
      {
          .topic = { .utf8 = start_topic, .size = strlen(start_topic) },
          .qos = MQTT_QOS_1_AT_LEAST_ONCE,
      },
      {
          .topic = { .utf8 = stop_topic, .size = strlen(stop_topic) },
          .qos = MQTT_QOS_1_AT_LEAST_ONCE,
      },
      {
          .topic = { .utf8 = registry_response_topic,
                     .size = strlen(registry_response_topic) },
          .qos = MQTT_QOS_1_AT_LEAST_ONCE,
      },
  };

  struct mqtt_subscription_list sub_list = {
      .list = topics,
      .list_count = ARRAY_SIZE(topics),
      .message_id = 1,
  };

  LOG_INF("Subscribing to topics for channel ID: %s", channel_id);

  int ret = mqtt_subscribe(&client_ctx, &sub_list);
  if (ret != 0) {
    LOG_ERR("Failed to subscribe to topics for channel ID: %s. Error: %d",
            channel_id, ret);
  } else {
    LOG_INF("Successfully subscribed to topics for channel ID: %s", channel_id);
  }

  return ret;
}

void handle_start_command(const char *payload)
{
  cJSON *json = cJSON_Parse(payload);
  if (json == NULL) {
    const char *error_ptr = cJSON_GetErrorPtr();
    if (error_ptr != NULL) {
      LOG_ERR("JSON parsing error before: %s", error_ptr);
    }
    return;
  }

  struct task t = {0};

  cJSON *id = cJSON_GetObjectItemCaseSensitive(json, "id");
  cJSON *name = cJSON_GetObjectItemCaseSensitive(json, "name");
  cJSON *image_url = cJSON_GetObjectItemCaseSensitive(json, "image_url");
  cJSON *file = cJSON_GetObjectItemCaseSensitive(json, "file");
  cJSON *inputs = cJSON_GetObjectItemCaseSensitive(json, "inputs");

  if (!cJSON_IsString(id) || !cJSON_IsString(name)) {
    LOG_ERR("Invalid or missing mandatory fields in JSON payload");
    cJSON_Delete(json);
    return;
  }

  strncpy(t.id, id->valuestring, MAX_ID_LEN - 1);
  strncpy(t.name, name->valuestring, MAX_NAME_LEN - 1);

  if (cJSON_IsString(image_url) && image_url->valuestring) {
    strncpy(t.image_url, image_url->valuestring, MAX_URL_LEN - 1);
  }

  if (cJSON_IsString(file) && file->valuestring) {
    strncpy(t.file, file->valuestring, MAX_BASE64_LEN - 1);
  }

  if (cJSON_IsArray(inputs)) {
    int input_count = cJSON_GetArraySize(inputs);
    t.inputs_count = (input_count > MAX_INPUTS) ? MAX_INPUTS : (size_t)input_count;
    for (size_t i = 0; i < t.inputs_count; ++i) {
      cJSON *input = cJSON_GetArrayItem(inputs, (int)i);
      if (cJSON_IsNumber(input)) {
        t.inputs[i] = (uint64_t)input->valuedouble;
      }
    }
  }

  LOG_INF("Starting task: ID=%s, Name=%s", t.id, t.name);
  LOG_INF("image_url=%s, file-len(b64)=%zu", t.image_url, strlen(t.file));
  LOG_INF("inputs_count=%zu", t.inputs_count);

  /* ---------------------------------------------------------------------- */
  /* FIX: execute with 't' (current task) not g_current_task before memcpy.  */
  /* ---------------------------------------------------------------------- */
  if (strlen(t.file) > 0) {
    size_t wasm_decoded_len = 0;
    static uint8_t wasm_binary[MAX_BASE64_LEN];

    int ret = base64_decode(wasm_binary, sizeof(wasm_binary), &wasm_decoded_len,
                            (const uint8_t *)t.file, strlen(t.file));
    if (ret < 0) {
      LOG_ERR("Failed to decode base64 WASM (task.file). Err=%d", ret);
      cJSON_Delete(json);
      return;
    }

    execute_wasm_module(t.id, wasm_binary, wasm_decoded_len,
                        t.inputs, t.inputs_count);

    t.file_len = wasm_decoded_len;

  } else if (strlen(t.image_url) > 0) {
    LOG_INF("Requesting WASM from registry: %s", t.image_url);
    extern const char *channel_id;
    extern const char *domain_id;
    publish_registry_request(domain_id, channel_id, t.image_url);
  } else {
    LOG_WRN("No file or image_url specified; cannot start WASM task!");
  }

  memcpy(&g_current_task, &t, sizeof(t));
  cJSON_Delete(json);
}

void handle_stop_command(const char *payload)
{
  cJSON *json = cJSON_Parse(payload);
  if (!json) {
    LOG_ERR("Failed to parse JSON payload");
    return;
  }

  struct task t;
  memset(&t, 0, sizeof(t));

  cJSON *id = cJSON_GetObjectItemCaseSensitive(json, "id");
  cJSON *name = cJSON_GetObjectItemCaseSensitive(json, "name");
  cJSON *state = cJSON_GetObjectItemCaseSensitive(json, "state");

  if (cJSON_IsString(id) && id->valuestring) {
    strncpy(t.id, id->valuestring, sizeof(t.id) - 1);
  } else {
    LOG_ERR("Invalid or missing 'id' field in stop command");
    cJSON_Delete(json);
    return;
  }

  if (cJSON_IsString(name) && name->valuestring) {
    strncpy(t.name, name->valuestring, sizeof(t.name) - 1);
  } else {
    LOG_ERR("Invalid or missing 'name' field in stop command");
    cJSON_Delete(json);
    return;
  }

  if (cJSON_IsString(state) && state->valuestring) {
    strncpy(t.state, state->valuestring, sizeof(t.state) - 1);
  } else {
    LOG_ERR("Invalid or missing 'state' field in stop command");
    cJSON_Delete(json);
    return;
  }

  LOG_INF("Stopping task: ID=%s, Name=%s, State=%s", t.id, t.name, t.state);

  stop_wasm_app(t.id);

  cJSON_Delete(json);
}

/* Handles a single chunk "data" field with the full base64 WASM. */
int handle_registry_response(const char *payload)
{
  cJSON *json = cJSON_Parse(payload);
  if (!json) {
    LOG_ERR("Failed to parse JSON payload");
    return -1;
  }

  struct registry_response resp;
  memset(&resp, 0, sizeof(resp));

  cJSON *app_name = cJSON_GetObjectItemCaseSensitive(json, "app_name");
  cJSON *data = cJSON_GetObjectItemCaseSensitive(json, "data");

  if (cJSON_IsString(app_name) && app_name->valuestring) {
    strncpy(resp.app_name, app_name->valuestring, sizeof(resp.app_name) - 1);
  } else {
    LOG_ERR("Invalid or missing 'app_name' field in registry response");
    cJSON_Delete(json);
    return -1;
  }

  if (cJSON_IsString(data) && data->valuestring) {
    strncpy(resp.data, data->valuestring, sizeof(resp.data) - 1);
  } else {
    LOG_ERR("Invalid or missing 'data' field in registry response");
    cJSON_Delete(json);
    return -1;
  }

  LOG_INF("Single-chunk registry response for app: %s", resp.app_name);

  size_t encoded_len = strlen(resp.data);
  size_t decoded_len = (encoded_len * 3) / 4; /* max possible decoded size */

  uint8_t *binary_data = malloc(decoded_len);
  if (!binary_data) {
    LOG_ERR("Failed to allocate memory for decoded binary");
    cJSON_Delete(json);
    return -1;
  }

  size_t actual_decoded_len = decoded_len;
  int ret = base64_decode(binary_data, decoded_len, &actual_decoded_len,
                          (const uint8_t *)resp.data, encoded_len);
  if (ret < 0) {
    LOG_ERR("Failed to decode Base64 data, err=%d", ret);
    free(binary_data);
    cJSON_Delete(json);
    return -1;
  }

  LOG_INF("Decoded single-chunk WASM size: %zu. Executing now...", actual_decoded_len);

  execute_wasm_module(g_current_task.id, binary_data, actual_decoded_len,
                      g_current_task.inputs, g_current_task.inputs_count);

  free(binary_data);
  cJSON_Delete(json);

  LOG_INF("WASM binary executed from single-chunk registry response.");
  return 0;
}

/* -------------------------------------------------------------------------- */
/* FIX #4: alive publishes the actual proplet_id you connected with            */
/* -------------------------------------------------------------------------- */
void publish_alive_message(const char *domain_id, const char *channel_id)
{
  char payload[192];

  const char *pid = (g_proplet_id[0] != '\0') ? g_proplet_id : CLIENT_ID;
  const char *ns = (g_namespace != NULL) ? g_namespace : DEFAULT_NAMESPACE;

  snprintf(payload, sizeof(payload),
           "{\"status\":\"alive\",\"proplet_id\":\"%s\",\"namespace\":\"%s\"}",
           pid, ns);

  (void)publish(domain_id, channel_id, ALIVE_TOPIC_TEMPLATE, payload);
}

void publish_metrics_message(const char *domain_id, const char *channel_id,
                             const char *proplet_id, const char *namespace)
{
  double cpu_percent = 0.0;

#if defined(CONFIG_CPU_LOAD)
  cpu_percent = (double)cpu_load_get(false) / 10.0;
#endif

  uint32_t heap_free = 0U;
  uint32_t heap_alloc = 0U;
  uint32_t heap_max_alloc = 0U;

#if defined(CONFIG_SYS_HEAP_RUNTIME_STATS)
  struct sys_memory_stats stat;
  extern struct sys_heap _system_heap;

  (void)sys_heap_runtime_stats_get(&_system_heap, &stat);
  heap_free = stat.free_bytes;
  heap_alloc = stat.allocated_bytes;
  heap_max_alloc = stat.max_allocated_bytes;
#endif

  cJSON *root = cJSON_CreateObject();
  if (root == NULL) {
    return;
  }

  cJSON_AddStringToObject(root, "proplet_id", proplet_id);
  cJSON_AddStringToObject(root, "namespace", namespace);

  cJSON *metrics = cJSON_AddObjectToObject(root, "metrics");
  if (metrics == NULL) {
    cJSON_Delete(root);
    return;
  }

  cJSON_AddStringToObject(metrics, "version", "v1");
  cJSON_AddNumberToObject(metrics, "timestamp_ms", (double)k_uptime_get());

  cJSON *cpu = cJSON_AddObjectToObject(metrics, "cpu");
  if (cpu != NULL) {
    cJSON_AddNumberToObject(cpu, "user_seconds", 0.0);
    cJSON_AddNumberToObject(cpu, "system_seconds", 0.0);
    cJSON_AddNumberToObject(cpu, "percent", cpu_percent);
  }

  cJSON *mem = cJSON_AddObjectToObject(metrics, "memory");
  if (mem != NULL) {
    cJSON_AddNumberToObject(mem, "rss_bytes", 0.0);
    cJSON_AddNumberToObject(mem, "heap_alloc_bytes", (double)heap_alloc);
    cJSON_AddNumberToObject(mem, "heap_sys_bytes", (double)(heap_alloc + heap_free));
    cJSON_AddNumberToObject(mem, "heap_inuse_bytes", (double)heap_alloc);
    cJSON_AddNumberToObject(mem, "heap_free_bytes", (double)heap_free);
    cJSON_AddNumberToObject(mem, "heap_max_alloc_bytes", (double)heap_max_alloc);
  }

  char *payload = cJSON_PrintUnformatted(root);
  if (payload != NULL) {
    (void)publish(domain_id, channel_id, METRICS_TOPIC_TEMPLATE, payload);
    cJSON_free(payload);
  }

  cJSON_Delete(root);
}

int publish_discovery(const char *domain_id, const char *proplet_id,
                      const char *channel_id)
{
  char topic[128];
  char payload[128];

  snprintf(topic, sizeof(topic), DISCOVERY_TOPIC_TEMPLATE, domain_id, channel_id);
  snprintf(payload, sizeof(payload), "{\"proplet_id\":\"%s\"}", proplet_id);

  if (!mqtt_connected) {
    LOG_ERR("MQTT client is not connected. Discovery aborted.");
    return -ENOTCONN;
  }

  struct mqtt_publish_param param;
  prepare_publish_param(&param, topic, payload);

  int ret = mqtt_publish(&client_ctx, &param);
  if (ret != 0) {
    LOG_ERR("Failed to publish discovery. Error: %d", ret);
    return ret;
  }

  LOG_INF("Discovery published successfully to topic: %s", topic);
  return 0;
}

void publish_registry_request(const char *domain_id, const char *channel_id,
                              const char *app_name)
{
  char payload[128];
  snprintf(payload, sizeof(payload), "{\"app_name\":\"%s\"}", app_name);

  if (publish(domain_id, channel_id, FETCH_REQUEST_TOPIC_TEMPLATE, payload) != 0) {
    LOG_ERR("Failed to request registry file");
  } else {
    LOG_INF("Requested registry file for: %s", app_name);
  }
}

void publish_results(const char *domain_id, const char *channel_id,
                     const char *task_id, const char *results)
{
  char results_payload[256];

  snprintf(results_payload, sizeof(results_payload),
           "{\"task_id\":\"%s\",\"results\":\"%s\"}", task_id, results);

  if (publish(domain_id, channel_id, RESULTS_TOPIC_TEMPLATE, results_payload) != 0) {
    LOG_ERR("Failed to publish results");
  } else {
    LOG_INF("Published results for task: %s", task_id);
  }
}

void mqtt_client_process(void)
{
  if (mqtt_connected) {
    int ret = poll_mqtt_socket(&client_ctx, mqtt_keepalive_time_left(&client_ctx));
    if (ret > 0) {
      mqtt_input(&client_ctx);
    }
    mqtt_live(&client_ctx);
  }
}
