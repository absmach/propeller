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
#include <zephyr/sys/util.h>

#if defined(CONFIG_CPU_LOAD)
#include <zephyr/debug/cpu_load.h>
#endif

#if defined(CONFIG_SYS_HEAP_RUNTIME_STATS)
#include <zephyr/sys/sys_heap.h>
#endif

LOG_MODULE_REGISTER(mqtt_client);

#define RX_BUFFER_SIZE 1024
#define TX_BUFFER_SIZE 1024

#if defined(PAYLOAD_BUFFER_SIZE) && (PAYLOAD_BUFFER_SIZE < 1024)
#undef PAYLOAD_BUFFER_SIZE
#define PAYLOAD_BUFFER_SIZE 1024
#endif

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

#define WILL_MESSAGE_TEMPLATE                                                  \
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
#define MAX_UPDATE_B64_LEN 2048
#define MAX_ERROR_MSG_LEN 256

struct fl_spec {
  char job_id[MAX_ID_LEN];
  uint64_t round_id;
  char global_version[MAX_ID_LEN];
  uint64_t min_participants;
  uint64_t round_timeout_sec;
  uint64_t clients_per_round;
  uint64_t total_rounds;
  char algorithm[MAX_NAME_LEN];
  char update_format[MAX_NAME_LEN];
  char model_ref[MAX_URL_LEN];
  uint64_t local_epochs;
  uint64_t batch_size;
  double learning_rate;
};

struct task {
  char id[MAX_ID_LEN];
  char name[MAX_NAME_LEN];
  char state[MAX_STATE_LEN];
  char image_url[MAX_URL_LEN];
  char mode[MAX_NAME_LEN];

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

  struct fl_spec fl;
  bool is_fl_task;
  
  char fl_job_id[MAX_ID_LEN];
  char fl_round_id_str[32];
  char fl_global_version[MAX_ID_LEN];
  char fl_global_update_b64[MAX_BASE64_LEN];
  char fl_format[MAX_NAME_LEN];
  char fl_num_samples_str[32];
  
  char round_id[MAX_ID_LEN];
  char model_uri[MAX_URL_LEN];
  char hyperparams[512];
  bool is_fml_task;
  
  char proplet_id[MAX_ID_LEN];
  char model_data[4096];
  char dataset_data[4096];
  char coordinator_url[MAX_URL_LEN];
  char model_registry_url[MAX_URL_LEN];
  char data_store_url[MAX_URL_LEN];
  bool model_data_fetched;
  bool dataset_data_fetched;
};

static struct task g_current_task;

static char g_proplet_id[MAX_ID_LEN];
static const char *g_namespace = DEFAULT_NAMESPACE;

static uint8_t rx_buffer[RX_BUFFER_SIZE];
static uint8_t tx_buffer[TX_BUFFER_SIZE];

static struct mqtt_client client_ctx;
static struct sockaddr_storage broker_addr;

static char g_will_topic_str[128];
static char g_will_message_str[256];
static struct mqtt_utf8 g_will_message;
static struct mqtt_topic g_will_topic;

static struct zsock_pollfd fds[1];
static int nfds;

bool mqtt_connected = false;

struct registry_response {
  char app_name[64];
  char data[MAX_BASE64_LEN];
};

static void prepare_fds(struct mqtt_client *client) {
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

static int poll_mqtt_socket(struct mqtt_client *client, int timeout) {
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
                               const struct mqtt_evt *evt) {
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

    snprintf(start_topic, sizeof(start_topic), START_TOPIC_TEMPLATE, domain_id,
             channel_id);
    snprintf(stop_topic, sizeof(stop_topic), STOP_TOPIC_TEMPLATE, domain_id,
             channel_id);
    snprintf(registry_response_topic, sizeof(registry_response_topic),
             REGISTRY_RESPONSE_TOPIC, domain_id, channel_id);

    LOG_INF("Message received on topic: %.*s", pub->message.topic.topic.size,
            pub->message.topic.topic.utf8);

    ret = mqtt_read_publish_payload(
        &client_ctx, payload,
        MIN(pub->message.payload.len, PAYLOAD_BUFFER_SIZE - 1));
    if (ret < 0) {
      LOG_ERR("Failed to read payload [%d]", ret);
      return;
    }
    payload[ret] = '\0'; /* Null-terminate */
    LOG_INF("Payload: %s", payload);

    const struct mqtt_utf8 *rtopic = &pub->message.topic.topic;
    char topic_str[256];
    snprintf(topic_str, sizeof(topic_str), "%.*s", (int)rtopic->size, rtopic->utf8);
    topic_str[sizeof(topic_str) - 1] = '\0';

    if (g_current_task.is_fml_task && strlen(g_current_task.model_uri) > 0 &&
        strcmp(topic_str, g_current_task.model_uri) == 0) {
      LOG_INF("Received model from topic: %s (payload: %d bytes)", topic_str, (int)pub->message.payload.len);
      
      size_t payload_size = MIN(pub->message.payload.len, sizeof(g_current_task.model_data) - 1);
      memcpy(g_current_task.model_data, payload, payload_size);
      g_current_task.model_data[payload_size] = '\0';
      g_current_task.model_data_fetched = true;
      LOG_INF("Model data stored (size: %zu bytes)", payload_size);
    } else if (rtopic->size == strlen(start_topic) &&
        memcmp(rtopic->utf8, start_topic, rtopic->size) == 0) {
      handle_start_command(payload);
    } else if (rtopic->size == strlen(stop_topic) &&
               memcmp(rtopic->utf8, stop_topic, rtopic->size) == 0) {
      handle_stop_command(payload);
    } else if (rtopic->size == strlen(registry_response_topic) &&
               memcmp(rtopic->utf8, registry_response_topic, rtopic->size) ==
                   0) {
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
                                  const char *topic_str, const char *payload) {
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

static int publish_direct(const char *topic, const char *payload) {
  if (!mqtt_connected) {
    LOG_ERR("MQTT client is not connected. Publish aborted.");
    return -ENOTCONN;
  }

  struct mqtt_publish_param param;
  prepare_publish_param(&param, topic, payload);

  int ret = mqtt_publish(&client_ctx, &param);
  if (ret != 0) {
    LOG_ERR("Failed to publish to topic %s. Error: %d", topic, ret);
    return ret;
  }

  LOG_INF("Published to topic: %s", topic);
  return 0;
}

int publish(const char *domain_id, const char *channel_id,
            const char *topic_template, const char *payload) {
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

int mqtt_client_connect(const char *domain_id, const char *proplet_id,
                        const char *channel_id) {
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

  strncpy(g_proplet_id, proplet_id, sizeof(g_proplet_id) - 1);
  g_proplet_id[sizeof(g_proplet_id) - 1] = '\0';

  snprintf(g_will_topic_str, sizeof(g_will_topic_str), ALIVE_TOPIC_TEMPLATE,
           domain_id, channel_id);

  snprintf(g_will_message_str, sizeof(g_will_message_str),
           WILL_MESSAGE_TEMPLATE, proplet_id, g_namespace);

  g_will_message.utf8 = (const uint8_t *)g_will_message_str;
  g_will_message.size = strlen(g_will_message_str);

  g_will_topic.topic.utf8 = (const uint8_t *)g_will_topic_str;
  g_will_topic.topic.size = strlen(g_will_topic_str);
  g_will_topic.qos = WILL_QOS;

  client_ctx.will_topic = &g_will_topic;
  client_ctx.will_message = &g_will_message;
  (void)WILL_RETAIN;

  client_ctx.broker = &broker_addr;
  client_ctx.evt_cb = mqtt_event_handler;
  client_ctx.client_id = MQTT_UTF8_LITERAL(CLIENT_ID);

  static struct mqtt_utf8 username;
  username.utf8 = (const uint8_t *)g_proplet_id;
  username.size = strlen(g_proplet_id);
  client_ctx.user_name = &username;

  client_ctx.password = NULL;
  client_ctx.protocol_version = MQTT_VERSION_3_1_1;

  client_ctx.rx_buf = rx_buffer;
  client_ctx.rx_buf_size = RX_BUFFER_SIZE;
  client_ctx.tx_buf = tx_buffer;
  client_ctx.tx_buf_size = TX_BUFFER_SIZE;

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

int subscribe(const char *domain_id, const char *channel_id) {
  static char start_topic[128];
  static char stop_topic[128];
  static char registry_response_topic[128];

  snprintf(start_topic, sizeof(start_topic), START_TOPIC_TEMPLATE, domain_id,
           channel_id);
  snprintf(stop_topic, sizeof(stop_topic), STOP_TOPIC_TEMPLATE, domain_id,
           channel_id);
  snprintf(registry_response_topic, sizeof(registry_response_topic),
           REGISTRY_RESPONSE_TOPIC, domain_id, channel_id);

  struct mqtt_topic topics[] = {
      {
          .topic = {.utf8 = start_topic, .size = strlen(start_topic)},
          .qos = MQTT_QOS_1_AT_LEAST_ONCE,
      },
      {
          .topic = {.utf8 = stop_topic, .size = strlen(stop_topic)},
          .qos = MQTT_QOS_1_AT_LEAST_ONCE,
      },
      {
          .topic = {.utf8 = registry_response_topic,
                    .size = strlen(registry_response_topic)},
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

void handle_start_command(const char *payload) {
  cJSON *json = cJSON_Parse(payload);
  if (json == NULL) {
    const char *error_ptr = cJSON_GetErrorPtr();
    if (error_ptr != NULL) {
      LOG_ERR("JSON parsing error before: %s", error_ptr);
    }
    return;
  }

  struct task t = {0};
  t.is_fl_task = false;

  cJSON *id = cJSON_GetObjectItemCaseSensitive(json, "id");
  cJSON *name = cJSON_GetObjectItemCaseSensitive(json, "name");
  cJSON *image_url = cJSON_GetObjectItemCaseSensitive(json, "image_url");
  cJSON *file = cJSON_GetObjectItemCaseSensitive(json, "file");
  cJSON *inputs = cJSON_GetObjectItemCaseSensitive(json, "inputs");
  cJSON *mode = cJSON_GetObjectItemCaseSensitive(json, "mode");
  cJSON *fl_obj = cJSON_GetObjectItemCaseSensitive(json, "fl");
  cJSON *env = cJSON_GetObjectItemCaseSensitive(json, "env");

  if (!cJSON_IsString(id) || !cJSON_IsString(name)) {
    LOG_ERR("Invalid or missing mandatory fields in JSON payload");
    cJSON_Delete(json);
    return;
  }

  strncpy(t.id, id->valuestring, MAX_ID_LEN - 1);
  t.id[MAX_ID_LEN - 1] = '\0';
  strncpy(t.name, name->valuestring, MAX_NAME_LEN - 1);
  t.name[MAX_NAME_LEN - 1] = '\0';

  if (cJSON_IsString(mode)) {
    strncpy(t.mode, mode->valuestring, MAX_NAME_LEN - 1);
    t.mode[MAX_NAME_LEN - 1] = '\0';
  }

  if (cJSON_IsString(image_url)) {
    strncpy(t.image_url, image_url->valuestring, MAX_URL_LEN - 1);
    t.image_url[MAX_URL_LEN - 1] = '\0';
  }

  if (cJSON_IsString(file)) {
    strncpy(t.file, file->valuestring, MAX_BASE64_LEN - 1);
    t.file[MAX_BASE64_LEN - 1] = '\0';
  }

  if (cJSON_IsArray(inputs)) {
    int input_count = cJSON_GetArraySize(inputs);
    t.inputs_count =
        (input_count > MAX_INPUTS) ? MAX_INPUTS : (size_t)input_count;
    for (size_t i = 0; i < t.inputs_count; ++i) {
      cJSON *input = cJSON_GetArrayItem(inputs, (int)i);
      if (cJSON_IsNumber(input)) {
        t.inputs[i] = (uint64_t)input->valuedouble;
      }
    }
  }

  if (fl_obj != NULL && cJSON_IsObject(fl_obj)) {
    t.is_fl_task = true;
    cJSON *job_id = cJSON_GetObjectItemCaseSensitive(fl_obj, "job_id");
    cJSON *round_id = cJSON_GetObjectItemCaseSensitive(fl_obj, "round_id");
    cJSON *global_version = cJSON_GetObjectItemCaseSensitive(fl_obj, "global_version");
    cJSON *update_format = cJSON_GetObjectItemCaseSensitive(fl_obj, "update_format");
    cJSON *min_participants = cJSON_GetObjectItemCaseSensitive(fl_obj, "min_participants");
    cJSON *total_rounds = cJSON_GetObjectItemCaseSensitive(fl_obj, "total_rounds");
    cJSON *local_epochs = cJSON_GetObjectItemCaseSensitive(fl_obj, "local_epochs");
    cJSON *batch_size = cJSON_GetObjectItemCaseSensitive(fl_obj, "batch_size");
    cJSON *learning_rate = cJSON_GetObjectItemCaseSensitive(fl_obj, "learning_rate");

    if (cJSON_IsString(job_id)) {
      strncpy(t.fl.job_id, job_id->valuestring, MAX_ID_LEN - 1);
      t.fl.job_id[MAX_ID_LEN - 1] = '\0';
      strncpy(t.fl_job_id, job_id->valuestring, MAX_ID_LEN - 1);
      t.fl_job_id[MAX_ID_LEN - 1] = '\0';
    }
    if (cJSON_IsNumber(round_id)) {
      t.fl.round_id = (uint64_t)round_id->valuedouble;
      snprintf(t.fl_round_id_str, sizeof(t.fl_round_id_str), "%llu", (unsigned long long)t.fl.round_id);
    }
    if (cJSON_IsString(global_version)) {
      strncpy(t.fl.global_version, global_version->valuestring, MAX_ID_LEN - 1);
      t.fl.global_version[MAX_ID_LEN - 1] = '\0';
      strncpy(t.fl_global_version, global_version->valuestring, MAX_ID_LEN - 1);
      t.fl_global_version[MAX_ID_LEN - 1] = '\0';
    }
    if (cJSON_IsString(update_format)) {
      strncpy(t.fl.update_format, update_format->valuestring, MAX_NAME_LEN - 1);
      t.fl.update_format[MAX_NAME_LEN - 1] = '\0';
      strncpy(t.fl_format, update_format->valuestring, MAX_NAME_LEN - 1);
      t.fl_format[MAX_NAME_LEN - 1] = '\0';
    }
    if (cJSON_IsNumber(min_participants)) {
      t.fl.min_participants = (uint64_t)min_participants->valuedouble;
    }
    if (cJSON_IsNumber(total_rounds)) {
      t.fl.total_rounds = (uint64_t)total_rounds->valuedouble;
    }
    if (cJSON_IsNumber(local_epochs)) {
      t.fl.local_epochs = (uint64_t)local_epochs->valuedouble;
    }
    if (cJSON_IsNumber(batch_size)) {
      t.fl.batch_size = (uint64_t)batch_size->valuedouble;
    }
    if (cJSON_IsNumber(learning_rate)) {
      t.fl.learning_rate = learning_rate->valuedouble;
    }
  }

  t.model_data_fetched = false;
  t.dataset_data_fetched = false;
  t.coordinator_url[0] = '\0';
  t.model_registry_url[0] = '\0';
  t.data_store_url[0] = '\0';
  t.model_data[0] = '\0';
  t.dataset_data[0] = '\0';
  
  const char *pid = (g_proplet_id[0] != '\0') ? g_proplet_id : CLIENT_ID;
  strncpy(t.proplet_id, pid, sizeof(t.proplet_id) - 1);
  t.proplet_id[sizeof(t.proplet_id) - 1] = '\0';

  if (env != NULL && cJSON_IsObject(env)) {
    cJSON *round_id_env = cJSON_GetObjectItemCaseSensitive(env, "ROUND_ID");
    cJSON *model_uri_env = cJSON_GetObjectItemCaseSensitive(env, "MODEL_URI");
    cJSON *hyperparams_env = cJSON_GetObjectItemCaseSensitive(env, "HYPERPARAMS");
    cJSON *coordinator_url_env = cJSON_GetObjectItemCaseSensitive(env, "COORDINATOR_URL");
    cJSON *model_registry_url_env = cJSON_GetObjectItemCaseSensitive(env, "MODEL_REGISTRY_URL");
    cJSON *data_store_url_env = cJSON_GetObjectItemCaseSensitive(env, "DATA_STORE_URL");
    
    if (cJSON_IsString(coordinator_url_env)) {
      strncpy(t.coordinator_url, coordinator_url_env->valuestring, sizeof(t.coordinator_url) - 1);
      t.coordinator_url[sizeof(t.coordinator_url) - 1] = '\0';
    } else {
      strncpy(t.coordinator_url, "http://coordinator-http:8080", sizeof(t.coordinator_url) - 1);
      t.coordinator_url[sizeof(t.coordinator_url) - 1] = '\0';
    }

    if (cJSON_IsString(model_registry_url_env)) {
      strncpy(t.model_registry_url, model_registry_url_env->valuestring, sizeof(t.model_registry_url) - 1);
      t.model_registry_url[sizeof(t.model_registry_url) - 1] = '\0';
    } else {
      strncpy(t.model_registry_url, "http://model-registry:8081", sizeof(t.model_registry_url) - 1);
      t.model_registry_url[sizeof(t.model_registry_url) - 1] = '\0';
    }

    if (cJSON_IsString(data_store_url_env)) {
      strncpy(t.data_store_url, data_store_url_env->valuestring, sizeof(t.data_store_url) - 1);
      t.data_store_url[sizeof(t.data_store_url) - 1] = '\0';
    } else {
      strncpy(t.data_store_url, "http://local-data-store:8083", sizeof(t.data_store_url) - 1);
      t.data_store_url[sizeof(t.data_store_url) - 1] = '\0';
    }

    if (cJSON_IsString(round_id_env)) {
      strncpy(t.round_id, round_id_env->valuestring, sizeof(t.round_id) - 1);
      t.round_id[sizeof(t.round_id) - 1] = '\0';
      t.is_fml_task = true;
      LOG_INF("FML task detected: ROUND_ID=%s", t.round_id);
    }

    if (cJSON_IsString(model_uri_env)) {
      strncpy(t.model_uri, model_uri_env->valuestring, sizeof(t.model_uri) - 1);
      t.model_uri[sizeof(t.model_uri) - 1] = '\0';
      LOG_INF("FML model URI: %s", t.model_uri);
    }

    if (cJSON_IsString(hyperparams_env)) {
      strncpy(t.hyperparams, hyperparams_env->valuestring, sizeof(t.hyperparams) - 1);
      t.hyperparams[sizeof(t.hyperparams) - 1] = '\0';
    }
    
    cJSON *fl_job_id_env = cJSON_GetObjectItemCaseSensitive(env, "FL_JOB_ID");
    cJSON *fl_round_id_env = cJSON_GetObjectItemCaseSensitive(env, "FL_ROUND_ID");
    cJSON *fl_global_version_env = cJSON_GetObjectItemCaseSensitive(env, "FL_GLOBAL_VERSION");
    cJSON *fl_global_update_env = cJSON_GetObjectItemCaseSensitive(env, "FL_GLOBAL_UPDATE_B64");
    cJSON *fl_format_env = cJSON_GetObjectItemCaseSensitive(env, "FL_FORMAT");
    cJSON *fl_num_samples_env = cJSON_GetObjectItemCaseSensitive(env, "FL_NUM_SAMPLES");

    if (cJSON_IsString(fl_job_id_env)) {
      strncpy(t.fl_job_id, fl_job_id_env->valuestring, MAX_ID_LEN - 1);
      t.fl_job_id[MAX_ID_LEN - 1] = '\0';
      if (!t.is_fl_task) {
        strncpy(t.fl.job_id, fl_job_id_env->valuestring, MAX_ID_LEN - 1);
        t.fl.job_id[MAX_ID_LEN - 1] = '\0';
        t.is_fl_task = true;
      }
    }
    if (cJSON_IsString(fl_round_id_env)) {
      strncpy(t.fl_round_id_str, fl_round_id_env->valuestring, sizeof(t.fl_round_id_str) - 1);
      t.fl_round_id_str[sizeof(t.fl_round_id_str) - 1] = '\0';
      t.fl.round_id = (uint64_t)strtoull(t.fl_round_id_str, NULL, 10);
    }
    if (cJSON_IsString(fl_global_version_env)) {
      strncpy(t.fl_global_version, fl_global_version_env->valuestring, MAX_ID_LEN - 1);
      t.fl_global_version[MAX_ID_LEN - 1] = '\0';
      if (!t.is_fl_task || strlen(t.fl.global_version) == 0) {
        strncpy(t.fl.global_version, fl_global_version_env->valuestring, MAX_ID_LEN - 1);
        t.fl.global_version[MAX_ID_LEN - 1] = '\0';
      }
    }
    if (cJSON_IsString(fl_global_update_env)) {
      strncpy(t.fl_global_update_b64, fl_global_update_env->valuestring, MAX_BASE64_LEN - 1);
      t.fl_global_update_b64[MAX_BASE64_LEN - 1] = '\0';
    }
    if (cJSON_IsString(fl_format_env)) {
      strncpy(t.fl_format, fl_format_env->valuestring, MAX_NAME_LEN - 1);
      t.fl_format[MAX_NAME_LEN - 1] = '\0';
      if (!t.is_fl_task || strlen(t.fl.update_format) == 0) {
        strncpy(t.fl.update_format, fl_format_env->valuestring, MAX_NAME_LEN - 1);
        t.fl.update_format[MAX_NAME_LEN - 1] = '\0';
      }
    }
    if (cJSON_IsString(fl_num_samples_env)) {
      strncpy(t.fl_num_samples_str, fl_num_samples_env->valuestring, sizeof(t.fl_num_samples_str) - 1);
      t.fl_num_samples_str[sizeof(t.fl_num_samples_str) - 1] = '\0';
    } else {
      strncpy(t.fl_num_samples_str, "1", sizeof(t.fl_num_samples_str) - 1);
      t.fl_num_samples_str[sizeof(t.fl_num_samples_str) - 1] = '\0';
    }
  }

  LOG_INF("Starting task: ID=%s, Name=%s, Mode=%s", t.id, t.name, t.mode);

  if (t.is_fml_task) {
    if (strlen(t.round_id) == 0) {
      LOG_ERR("FML task missing required ROUND_ID field");
      cJSON_Delete(json);
      return;
    }
    LOG_INF("FML Task: round_id=%s, model_uri=%s", t.round_id, t.model_uri);
    if (strlen(t.model_uri) > 0) {
      struct mqtt_topic model_topic = {
        .topic = {.utf8 = (char *)t.model_uri, .size = strlen(t.model_uri)},
        .qos = MQTT_QOS_1_AT_LEAST_ONCE,
      };
      struct mqtt_subscription_list sub_list = {
        .list = &model_topic,
        .list_count = 1,
        .message_id = sys_rand32_get() & 0xFFFF,
      };
      int ret = mqtt_subscribe(&client_ctx, &sub_list);
      if (ret == 0) {
        LOG_INF("Subscribed to model topic: %s", t.model_uri);
      } else {
        LOG_ERR("Failed to subscribe to model topic: %s (error: %d)", t.model_uri, ret);
      }
    }
  } else if (t.is_fl_task) {
    if (strlen(t.fl.job_id) == 0) {
      LOG_ERR("FL task (legacy) missing required job_id field");
      cJSON_Delete(json);
      return;
    }
    LOG_INF("FL Task (legacy): job_id=%s, round_id=%llu, global_version=%s, format=%s",
            t.fl.job_id, (unsigned long long)t.fl.round_id, 
            t.fl.global_version, t.fl.update_format);
  }
  LOG_INF("image_url=%s, file-len(b64)=%zu", t.image_url, strlen(t.file));
  LOG_INF("inputs_count=%zu", t.inputs_count);

  memcpy(&g_current_task, &t, sizeof(t));

  if (t.is_fml_task && strlen(t.model_uri) > 0) {
    int model_version = extract_model_version_from_uri(t.model_uri);
    char model_url[512];
    snprintf(model_url, sizeof(model_url), "%s/models/%d", 
             t.model_registry_url, model_version);
    
    LOG_INF("FL/FML task: Fetching model from registry: %s", model_url);
    
    char http_response[4096];
    if (http_get_json(model_url, http_response, sizeof(http_response)) == 0) {
      size_t response_len = strlen(http_response);
      if (response_len >= sizeof(g_current_task.model_data) - 1) {
        LOG_ERR("Model data truncated (size >= %zu), training may fail", sizeof(g_current_task.model_data));
      }
      strncpy(g_current_task.model_data, http_response,
              sizeof(g_current_task.model_data) - 1);
      g_current_task.model_data[sizeof(g_current_task.model_data) - 1] = '\0';
      g_current_task.model_data_fetched = true;
      LOG_INF("Successfully fetched model v%d via HTTP and stored in MODEL_DATA", model_version);
    } else {
      LOG_WRN("HTTP model fetch failed, will use MQTT subscription for model topic: %s", t.model_uri);
    }
  }

  if (t.is_fml_task && strlen(t.proplet_id) > 0) {
    char dataset_url[512];
    snprintf(dataset_url, sizeof(dataset_url), "%s/datasets/%s", 
             t.data_store_url, t.proplet_id);
    
    LOG_INF("FL/FML task: Fetching dataset for proplet_id=%s from: %s", 
            t.proplet_id, dataset_url);
    
    char http_response[4096];
    if (http_get_json(dataset_url, http_response, sizeof(http_response)) == 0) {
      size_t response_len = strlen(http_response);
      if (response_len >= sizeof(g_current_task.dataset_data) - 1) {
        LOG_ERR("Dataset data truncated (size >= %zu), training may fail", sizeof(g_current_task.dataset_data));
      }
      strncpy(g_current_task.dataset_data, http_response,
              sizeof(g_current_task.dataset_data) - 1);
      g_current_task.dataset_data[sizeof(g_current_task.dataset_data) - 1] = '\0';
      g_current_task.dataset_data_fetched = true;
      LOG_INF("Successfully fetched dataset via HTTP and stored in DATASET_DATA");
    } else {
      LOG_WRN("HTTP dataset fetch failed for proplet_id=%s (WASM client may use synthetic data)", 
              t.proplet_id);
    }
  }

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

    g_current_task.file_len = wasm_decoded_len;

    execute_wasm_module(t.id, wasm_binary, wasm_decoded_len, t.inputs,
                        t.inputs_count);

  } else if (strlen(t.image_url) > 0) {
    LOG_INF("Requesting WASM from registry: %s", t.image_url);
    extern const char *channel_id;
    extern const char *domain_id;
    publish_registry_request(domain_id, channel_id, t.image_url);
  } else {
    LOG_WRN("No file or image_url specified; cannot start WASM task!");
  }
  cJSON_Delete(json);
}

void handle_stop_command(const char *payload) {
  cJSON *json = cJSON_Parse(payload);
  if (!json) {
    LOG_ERR("Failed to parse JSON payload");
    return;
  }

  struct task t;
  memset(&t, 0, sizeof(t));
  t.is_fml_task = false;
  t.is_fl_task = false;

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

static int http_get_json(const char *url, char *response_buffer, size_t buffer_size) {
  int sock = zsock_socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
  if (sock < 0) {
    LOG_ERR("Failed to create socket for HTTP GET: %d", sock);
    return -1;
  }

  char host[256] = {0};
  int port = 80;
  char path[512] = {0};
  
  if (strncmp(url, "http://", 7) != 0) {
    LOG_ERR("Only http:// URLs are supported, got: %s", url);
    zsock_close(sock);
    return -1;
  }
  
  const char *host_start = url + 7;
  const char *port_start = strchr(host_start, ':');
  const char *path_start = strchr(host_start, '/');
  
  if (port_start && (!path_start || port_start < path_start)) {
    size_t host_len = port_start - host_start;
    strncpy(host, host_start, MIN(host_len, sizeof(host) - 1));
    port = atoi(port_start + 1);
    
    if (path_start) {
      strncpy(path, path_start, MIN(strlen(path_start), sizeof(path) - 1));
    } else {
      strcpy(path, "/");
    }
  } else if (path_start) {
    size_t host_len = path_start - host_start;
    strncpy(host, host_start, MIN(host_len, sizeof(host) - 1));
    strncpy(path, path_start, MIN(strlen(path_start), sizeof(path) - 1));
  } else {
    strncpy(host, host_start, MIN(strlen(host_start), sizeof(host) - 1));
    strcpy(path, "/");
  }

  struct sockaddr_in addr;
  addr.sin_family = AF_INET;
  addr.sin_port = htons(port);
  
  int ret = net_addr_pton(AF_INET, host, &addr.sin_addr);
  if (ret != 0) {
    LOG_ERR("Failed to resolve hostname %s: %d", host, ret);
    zsock_close(sock);
    return -1;
  }

  ret = zsock_connect(sock, (struct sockaddr *)&addr, sizeof(addr));
  if (ret < 0) {
    LOG_ERR("Failed to connect to %s:%d: %d", host, port, ret);
    zsock_close(sock);
    return -1;
  }

  struct timeval tv;
  tv.tv_sec = 30;
  tv.tv_usec = 0;
  ret = zsock_setsockopt(sock, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
  if (ret < 0) {
    LOG_WRN("Failed to set socket receive timeout: %d", ret);
  }

  char request[2048];
  int request_len = snprintf(request, sizeof(request),
           "GET %s HTTP/1.1\r\n"
           "Host: %s\r\n"
           "Connection: close\r\n"
           "\r\n",
           path, host);

  if (request_len < 0 || request_len >= (int)sizeof(request)) {
    LOG_ERR("HTTP request too long or formatting error: %d", request_len);
    zsock_close(sock);
    return -1;
  }
  
  ret = zsock_send(sock, request, strlen(request), 0);
  if (ret < 0) {
    LOG_ERR("Failed to send HTTP request: %d", ret);
    zsock_close(sock);
    return -1;
  }

  size_t total_received = 0;
  bool headers_complete = false;
  size_t content_length = 0;
  char header_buffer[1024] = {0};
  size_t header_buffer_len = 0;
  int http_status_code = 0;
  
  while (total_received < buffer_size - 1) {
    char chunk[512];
    ret = zsock_recv(sock, chunk, sizeof(chunk) - 1, 0);
    if (ret <= 0) {
      break;
    }
    chunk[ret] = '\0';
    
    if (!headers_complete) {
      size_t header_space = sizeof(header_buffer) - header_buffer_len - 1;
      size_t copy_len = MIN((size_t)ret, header_space);
      memcpy(header_buffer + header_buffer_len, chunk, copy_len);
      header_buffer_len += copy_len;
      header_buffer[header_buffer_len] = '\0';

      char *header_end = strstr(header_buffer, "\r\n\r\n");
      if (header_end) {
        headers_complete = true;
        size_t header_len = (header_end - header_buffer) + 4;

        char *status_line = header_buffer;
        char *http_version_end = strchr(status_line, ' ');
        if (http_version_end) {
          char *status_code_start = http_version_end + 1;
          char *status_code_end = strchr(status_code_start, ' ');
          if (status_code_end) {
            char status_code_str[16];
            size_t code_len = status_code_end - status_code_start;
            if (code_len < sizeof(status_code_str)) {
              memcpy(status_code_str, status_code_start, code_len);
              status_code_str[code_len] = '\0';
              http_status_code = atoi(status_code_str);
              LOG_INF("HTTP status code: %d", http_status_code);
            }
          }
        }

        if (http_status_code < 200 || http_status_code >= 300) {
          LOG_ERR("HTTP request failed with status: %d", http_status_code);
          zsock_close(sock);
          return -1;
        }

        size_t body_start_in_chunk = 0;
        if (header_len > (header_buffer_len - copy_len)) {
          body_start_in_chunk = header_len - header_buffer_len + copy_len;
        }

        char *cl_header = strstr(header_buffer, "Content-Length:");
        if (cl_header) {
          const char *val_start = cl_header + strlen("Content-Length:");
          char *endptr;
          unsigned long cl_val = strtoul(val_start, &endptr, 10);
          if (endptr != val_start && cl_val <= (unsigned long)SIZE_MAX) {
            content_length = (size_t)cl_val;
          }
        }

        if (body_start_in_chunk < (size_t)ret) {
          size_t body_len = ret - body_start_in_chunk;
          if (total_received + body_len < buffer_size - 1) {
            memcpy(response_buffer + total_received, chunk + body_start_in_chunk, body_len);
            total_received += body_len;
          }
        }
      } else if (header_buffer_len >= sizeof(header_buffer) - 1) {
        LOG_ERR("HTTP headers too long or malformed");
        zsock_close(sock);
        return -1;
      }
    } else {
      size_t copy_len = MIN((size_t)ret, buffer_size - total_received - 1);
      memcpy(response_buffer + total_received, chunk, copy_len);
      total_received += copy_len;
    }
    
    if (content_length > 0 && total_received >= content_length) {
      break;
    }
  }
  
  response_buffer[total_received] = '\0';
  zsock_close(sock);
  
  if (total_received == 0) {
    LOG_WRN("No data received from HTTP GET %s", url);
    return -1;
  }
  
  LOG_INF("HTTP GET %s successful, received %zu bytes", url, total_received);
  return 0;
}

static int extract_model_version_from_uri(const char *uri) {
  if (!uri || strlen(uri) == 0) {
    return 0;
  }
  
  const char *last_part = strrchr(uri, '/');
  if (!last_part) {
    last_part = uri;
  } else {
    last_part++;
  }
  
  const char *v_prefix = strstr(last_part, "global_model_v");
  if (v_prefix) {
    const char *version_str = v_prefix + strlen("global_model_v");
    int version = atoi(version_str);
    return version;
  }
  
  int version = 0;
  for (size_t i = 0; i < strlen(last_part); i++) {
    if (last_part[i] >= '0' && last_part[i] <= '9') {
      version = (version * 10) + (last_part[i] - '0');
    } else if (version > 0) {
      break;
    }
  }
  
  return version;
}

int handle_registry_response(const char *payload) {
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

  LOG_INF("Decoded single-chunk WASM size: %zu. Executing now...",
          actual_decoded_len);

  execute_wasm_module(g_current_task.id, binary_data, actual_decoded_len,
                      g_current_task.inputs, g_current_task.inputs_count);

  free(binary_data);
  cJSON_Delete(json);

  LOG_INF("WASM binary executed from single-chunk registry response.");
  return 0;
}

void publish_alive_message(const char *domain_id, const char *channel_id) {
  char payload[192];

  const char *pid = (g_proplet_id[0] != '\0') ? g_proplet_id : CLIENT_ID;
  const char *ns = (g_namespace != NULL) ? g_namespace : DEFAULT_NAMESPACE;

  snprintf(payload, sizeof(payload),
           "{\"status\":\"alive\",\"proplet_id\":\"%s\",\"namespace\":\"%s\"}",
           pid, ns);

  (void)publish(domain_id, channel_id, ALIVE_TOPIC_TEMPLATE, payload);
}

void publish_metrics_message(const char *domain_id, const char *channel_id,
                             const char *proplet_id, const char *namespace) {
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
  cJSON_AddNumberToObject(root, "timestamp", (double)k_uptime_get());

  cJSON *cpu = cJSON_AddObjectToObject(root, "cpu_metrics");
  if (cpu != NULL) {
    cJSON_AddNumberToObject(cpu, "user_seconds", 0.0);
    cJSON_AddNumberToObject(cpu, "system_seconds", 0.0);
    cJSON_AddNumberToObject(cpu, "percent", cpu_percent);
  }

  cJSON *mem = cJSON_AddObjectToObject(root, "memory_metrics");
  if (mem != NULL) {
    cJSON_AddNumberToObject(mem, "rss_bytes", 0.0);
    cJSON_AddNumberToObject(mem, "heap_alloc_bytes", (double)heap_alloc);
    cJSON_AddNumberToObject(mem, "heap_sys_bytes",
                            (double)(heap_alloc + heap_free));
    cJSON_AddNumberToObject(mem, "heap_inuse_bytes", (double)heap_alloc);
    cJSON_AddNumberToObject(mem, "heap_free_bytes", (double)heap_free);
    cJSON_AddNumberToObject(mem, "heap_max_alloc_bytes",
                            (double)heap_max_alloc);
  }

  char *payload = cJSON_PrintUnformatted(root);
  if (payload != NULL) {
    (void)publish(domain_id, channel_id, METRICS_TOPIC_TEMPLATE, payload);
    cJSON_free(payload);
  }

  cJSON_Delete(root);
}

int publish_discovery(const char *domain_id, const char *proplet_id,
                      const char *channel_id) {
  char topic[128];
  char payload[128];

  snprintf(topic, sizeof(topic), DISCOVERY_TOPIC_TEMPLATE, domain_id,
           channel_id);
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
                              const char *app_name) {
  char payload[128];
  snprintf(payload, sizeof(payload), "{\"app_name\":\"%s\"}", app_name);

  if (publish(domain_id, channel_id, FETCH_REQUEST_TOPIC_TEMPLATE, payload) !=
      0) {
    LOG_ERR("Failed to request registry file");
  } else {
    LOG_INF("Requested registry file for: %s", app_name);
  }
}

void publish_results(const char *domain_id, const char *channel_id,
                     const char *task_id, const char *results) {
  publish_results_with_error(domain_id, channel_id, task_id, results, NULL);
}

static void json_escape_string(char *dest, size_t dest_size, const char *src) {
  if (!src || dest_size == 0) {
    if (dest_size > 0) {
      dest[0] = '\0';
    }
    return;
  }

  size_t j = 0;
  for (size_t i = 0; src[i] != '\0' && j < dest_size - 1; i++) {
    switch (src[i]) {
      case '"':
        if (j + 2 < dest_size) {
          dest[j++] = '\\';
          dest[j++] = '"';
        }
        break;
      case '\\':
        if (j + 2 < dest_size) {
          dest[j++] = '\\';
          dest[j++] = '\\';
        }
        break;
      case '\b':
        if (j + 2 < dest_size) {
          dest[j++] = '\\';
          dest[j++] = 'b';
        }
        break;
      case '\f':
        if (j + 2 < dest_size) {
          dest[j++] = '\\';
          dest[j++] = 'f';
        }
        break;
      case '\n':
        if (j + 2 < dest_size) {
          dest[j++] = '\\';
          dest[j++] = 'n';
        }
        break;
      case '\r':
        if (j + 2 < dest_size) {
          dest[j++] = '\\';
          dest[j++] = 'r';
        }
        break;
      case '\t':
        if (j + 2 < dest_size) {
          dest[j++] = '\\';
          dest[j++] = 't';
        }
        break;
      default:
        if (j < dest_size - 1) {
          dest[j++] = src[i];
        }
        break;
    }
  }
  dest[j] = '\0';
}

void publish_results_with_error(const char *domain_id, const char *channel_id,
                                 const char *task_id, const char *results,
                                 const char *error_msg) {
  char results_payload[2048];
  char escaped_error[MAX_ERROR_MSG_LEN * 2];
  const char *pid = (g_proplet_id[0] != '\0') ? g_proplet_id : CLIENT_ID;

  if (error_msg) {
    json_escape_string(escaped_error, sizeof(escaped_error), error_msg);
  } else {
    escaped_error[0] = '\0';
  }

  if (g_current_task.is_fml_task && strlen(g_current_task.round_id) > 0) {
    cJSON *update_json = NULL;
    if (results && strlen(results) > 0) {
      update_json = cJSON_Parse(results);
    }
    
    char update_topic[256];
    snprintf(update_topic, sizeof(update_topic), "fl/rounds/%s/updates/%s",
             g_current_task.round_id, pid);
    
    if (error_msg) {
      snprintf(results_payload, sizeof(results_payload),
        "{"
        "\"round_id\":\"%s\","
        "\"proplet_id\":\"%s\","
        "\"base_model_uri\":\"%s\","
        "\"num_samples\":0,"
        "\"metrics\":{},"
        "\"update\":{},"
        "\"error\":\"%s\""
        "}",
        g_current_task.round_id,
        pid,
        g_current_task.model_uri,
        escaped_error
      );
    } else if (update_json) {
      cJSON *round_id_obj = cJSON_GetObjectItemCaseSensitive(update_json, "round_id");
      cJSON *proplet_id_obj = cJSON_GetObjectItemCaseSensitive(update_json, "proplet_id");
      cJSON *base_model_uri_obj = cJSON_GetObjectItemCaseSensitive(update_json, "base_model_uri");
      
      if (!round_id_obj) {
        cJSON_AddStringToObject(update_json, "round_id", g_current_task.round_id);
      }
      if (!proplet_id_obj) {
        cJSON_AddStringToObject(update_json, "proplet_id", pid);
      }
      if (!base_model_uri_obj && strlen(g_current_task.model_uri) > 0) {
        cJSON_AddStringToObject(update_json, "base_model_uri", g_current_task.model_uri);
      }
      
      char *json_str = cJSON_PrintUnformatted(update_json);
      if (json_str) {
        strncpy(results_payload, json_str, sizeof(results_payload));
        cJSON_free(json_str);
      } else {
        snprintf(results_payload, sizeof(results_payload),
          "{\"round_id\":\"%s\",\"proplet_id\":\"%s\",\"base_model_uri\":\"%s\",\"num_samples\":0,\"metrics\":{},\"update\":{}}",
          g_current_task.round_id, pid, g_current_task.model_uri);
      }
      cJSON_Delete(update_json);
    } else {
      snprintf(results_payload, sizeof(results_payload),
        "{"
        "\"round_id\":\"%s\","
        "\"proplet_id\":\"%s\","
        "\"base_model_uri\":\"%s\","
        "\"num_samples\":0,"
        "\"metrics\":{},"
        "\"update\":{}"
        "}",
        g_current_task.round_id,
        pid,
        g_current_task.model_uri
      );
    }
    
    if (publish_direct(update_topic, results_payload) != 0) {
      LOG_ERR("Failed to publish FML update to %s", update_topic);
    } else {
      LOG_INF("Published FML update to %s", update_topic);
    }
    return;
  }

  if (g_current_task.is_fl_task && 
      strcmp(g_current_task.mode, "train") == 0 &&
      strlen(g_current_task.fl.job_id) > 0) {
    if (strlen(g_current_task.fl.global_version) == 0) {
      LOG_ERR("FL task missing global_version, cannot publish update");
      return;
    }

    char update_b64[MAX_UPDATE_B64_LEN];
    size_t encoded_len = 0;
    
    size_t results_len = results ? strlen(results) : 0;
    if (results_len == 0 && !error_msg) {
      LOG_ERR("FL task has empty results and no error message");
      return;
    }

    if (results_len > (MAX_UPDATE_B64_LEN * 3 / 4)) {
      LOG_ERR("FL update payload too large: %zu bytes (max: %d)", 
              results_len, (MAX_UPDATE_B64_LEN * 3 / 4));
      error_msg = "Update payload exceeds size limit";
      results_len = 0;
    }

    if (results_len > 0) {
      int ret = base64_encode((uint8_t *)update_b64, sizeof(update_b64), &encoded_len,
                             (const uint8_t *)results, results_len);
      if (ret < 0) {
        LOG_ERR("Failed to encode results as base64");
        error_msg = "Failed to encode update as base64";
        encoded_len = 0;
        update_b64[0] = '\0';
      } else if (encoded_len == 0) {
        LOG_ERR("Base64 encoding produced empty result");
        error_msg = "Base64 encoding produced empty result";
        update_b64[0] = '\0';
      } else {
        update_b64[encoded_len] = '\0';
      }
    } else {
      if (!error_msg) {
        LOG_ERR("FL task has no results and no error message");
        error_msg = "No results data available";
      }
      encoded_len = 0;
      update_b64[0] = '\0';
    }

    const char *pid = (g_proplet_id[0] != '\0') ? g_proplet_id : CLIENT_ID;
    const char *format = (strlen(g_current_task.fl_format) > 0) ? 
                        g_current_task.fl_format : "f32-delta";
    uint64_t num_samples = strtoull(g_current_task.fl_num_samples_str, NULL, 10);
    if (num_samples == 0) {
      num_samples = 1;
    }

    if (error_msg) {
      snprintf(results_payload, sizeof(results_payload),
        "{"
        "\"task_id\":\"%s\","
        "\"results\":{"
          "\"task_id\":\"%s\","
          "\"job_id\":\"%s\","
          "\"round_id\":%llu,"
          "\"global_version\":\"%s\","
          "\"proplet_id\":\"%s\","
          "\"num_samples\":%llu,"
          "\"update_b64\":\"\","
          "\"format\":\"%s\","
          "\"metrics\":{}"
        "},"
        "\"error\":\"%s\""
        "}",
        task_id,
        task_id,
        g_current_task.fl.job_id,
        (unsigned long long)g_current_task.fl.round_id,
        g_current_task.fl.global_version,
        pid,
        (unsigned long long)num_samples,
        format,
        escaped_error
      );
    } else {
      snprintf(results_payload, sizeof(results_payload),
        "{"
        "\"task_id\":\"%s\","
        "\"results\":{"
          "\"task_id\":\"%s\","
          "\"job_id\":\"%s\","
          "\"round_id\":%llu,"
          "\"global_version\":\"%s\","
          "\"proplet_id\":\"%s\","
          "\"num_samples\":%llu,"
          "\"update_b64\":\"%s\","
          "\"format\":\"%s\","
          "\"metrics\":{}"
        "}"
        "}",
        task_id,
        task_id,
        g_current_task.fl.job_id,
        (unsigned long long)g_current_task.fl.round_id,
        g_current_task.fl.global_version,
        pid,
        (unsigned long long)num_samples,
        update_b64,
        format
      );
    }

    LOG_INF("Publishing FL update envelope for task: %s (job=%s, round=%llu, error=%s)",
            task_id, g_current_task.fl.job_id, (unsigned long long)g_current_task.fl.round_id,
            error_msg ? error_msg : "none");
  } else {
    char escaped_results[2048];
    if (results) {
      json_escape_string(escaped_results, sizeof(escaped_results), results);
    } else {
      escaped_results[0] = '\0';
    }
    
    if (error_msg) {
      snprintf(results_payload, sizeof(results_payload),
               "{\"task_id\":\"%s\",\"results\":\"%s\",\"error\":\"%s\"}", 
               task_id, escaped_results, escaped_error);
    } else {
      snprintf(results_payload, sizeof(results_payload),
               "{\"task_id\":\"%s\",\"results\":\"%s\"}", task_id, escaped_results);
    }
  }

  if (publish(domain_id, channel_id, RESULTS_TOPIC_TEMPLATE, results_payload) !=
      0) {
    LOG_ERR("Failed to publish results");
  } else {
    LOG_INF("Published results for task: %s", task_id);
  }
}

void mqtt_client_process(void) {
  if (mqtt_connected) {
    int ret =
        poll_mqtt_socket(&client_ctx, mqtt_keepalive_time_left(&client_ctx));
    if (ret > 0) {
      mqtt_input(&client_ctx);
    }
    mqtt_live(&client_ctx);
  }
}
