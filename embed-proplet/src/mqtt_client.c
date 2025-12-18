#include "mqtt_client.h"
#include "cJSON.h"
#include "net/mqtt.h"
#include "wasm_handler.h"
#include "crypto_utils.h"
#include "certs.h"
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>
#include <zephyr/net/socket.h>
#include <zephyr/net/tls_credentials.h>
#include <zephyr/random/random.h>
#include <zephyr/storage/disk_access.h>
#include <zephyr/sys/base64.h>
#include <mbedtls/sha256.h>

LOG_MODULE_REGISTER(mqtt_client);

#define RX_BUFFER_SIZE 2048 
#define TX_BUFFER_SIZE 2048

#define MQTT_BROKER_HOSTNAME "10.42.0.1" 
#define MQTT_BROKER_PORT 8883            
#define TLS_SEC_TAG 42                   

#define REGISTRY_ACK_TOPIC_TEMPLATE "m/%s/c/%s/control/manager/registry"
#define ALIVE_TOPIC_TEMPLATE "m/%s/c/%s/control/proplet/alive"
#define DISCOVERY_TOPIC_TEMPLATE "m/%s/c/%s/control/proplet/create"
#define START_TOPIC_TEMPLATE "m/%s/c/%s/control/manager/start"
#define STOP_TOPIC_TEMPLATE "m/%s/c/%s/control/manager/stop"
#define REGISTRY_RESPONSE_TOPIC "m/%s/c/%s/registry/server"
#define FETCH_REQUEST_TOPIC_TEMPLATE "m/%s/c/%s/registry/proplet"
#define RESULTS_TOPIC_TEMPLATE "m/%s/c/%s/control/proplet/results"

#define WILL_MESSAGE_TEMPLATE                                                  \
  "{\"status\":\"offline\",\"proplet_id\":\"%s\",\"namespace\":\"%s\"}"
#define WILL_QOS MQTT_QOS_1_AT_LEAST_ONCE
#define WILL_RETAIN 1

#define CLIENT_ID "proplet-esp32s3"
#define PROPLET_ID "<YOUR_PROPLET_ID>"
#define PROPLET_PASSWORD "<YOUR_PROPLET_PASSWORD>"
#define K8S_NAMESPACE "default"

#define MAX_ID_LEN 64
#define MAX_NAME_LEN 64
#define MAX_STATE_LEN 16
#define MAX_URL_LEN 256
#define MAX_TIMESTAMP_LEN 32
#define MAX_BASE64_LEN 4096 
#define MAX_INPUTS 16
#define MAX_RESULTS 16

#define MAX_WASM_FILE_SIZE (1536 * 1024) 

/* Workload Encryption Key */
static const uint8_t workload_key[32] = {
    0xdd, 0x72, 0x84, 0xe5, 0x6c, 0xb4, 0xa0, 0xde,
    0x0e, 0x28, 0xcb, 0x0d, 0x10, 0x0c, 0x1a, 0x2c,
    0xc6, 0xf6, 0x45, 0xdc, 0x03, 0x7b, 0x43, 0xa8,
    0x3e, 0xd3, 0xad, 0x7a, 0x16, 0x65, 0x5f, 0x53
};

/* Reassembly Globals */
static uint8_t *assembly_buffer = NULL;
static size_t assembly_cursor = 0;
static int expected_chunk_idx = 0;
static char expected_checksum[65] = {0};

/* Active Task */
static struct task g_current_task;

static uint8_t rx_buffer[RX_BUFFER_SIZE];
static uint8_t tx_buffer[TX_BUFFER_SIZE];

static struct mqtt_client client_ctx;
static struct sockaddr_storage broker_addr;
static sec_tag_t m_sec_tags[] = { TLS_SEC_TAG };

static struct zsock_pollfd fds[1];
static int nfds;

bool mqtt_connected = false;

struct task {
struct task {
  char id[MAX_ID_LEN];
  char name[MAX_NAME_LEN];
  char state[MAX_STATE_LEN];
  char image_url[MAX_URL_LEN];
  char *file;
  size_t file_len;
  uint64_t inputs[MAX_INPUTS];
  size_t inputs_count;
  uint64_t results[MAX_RESULTS];
  size_t results_count;
};

static int verify_checksum(const uint8_t *data, size_t len, const char *expected_hex) {
    if (!expected_hex || strlen(expected_hex) != 64) {
        LOG_WRN("Checksum skipped: invalid/missing expected checksum");
        return 0; 
    }

    uint8_t hash[32];
    mbedtls_sha256_context ctx;

    mbedtls_sha256_init(&ctx);
    mbedtls_sha256_starts(&ctx, 0); // 0 = SHA-256
    mbedtls_sha256_update(&ctx, data, len);
    mbedtls_sha256_finish(&ctx, hash);
    mbedtls_sha256_free(&ctx);

    char calculated_hex[65];
    for(int i = 0; i < 32; i++) {
        sprintf(calculated_hex + (i * 2), "%02x", hash[i]);
    }
    calculated_hex[64] = '\0';

    if (strncmp(calculated_hex, expected_hex, 64) != 0) {
        LOG_ERR("Checksum mismatch! Expected: %s, Got: %s", expected_hex, calculated_hex);
        return -1;
    }
    LOG_INF("Checksum verified.");
    return 0;
}

static int persist_workload(const char *task_id, const uint8_t *data, size_t len) {
    // [Optional] Implement filesystem write here using <zephyr/fs/fs.h>
    // For now, we use RAM-only execution for speed and memory safety on flash wear.
    LOG_DBG("Persisting task %s (Size: %zu) - RAM ONLY mode active", task_id, len);
    return 0;
}

static void prepare_fds(struct mqtt_client *client) {
  if (client->transport.type == MQTT_TRANSPORT_NON_SECURE) {
static void prepare_fds(struct mqtt_client *client) {
  if (client->transport.type == MQTT_TRANSPORT_NON_SECURE) {
    fds[0].fd = client->transport.tcp.sock;
  }
#if defined(CONFIG_MQTT_LIB_TLS)
  else if (client->transport.type == MQTT_TRANSPORT_SECURE) {
  else if (client->transport.type == MQTT_TRANSPORT_SECURE) {
    fds[0].fd = client->transport.tls.sock;
  }
#endif
  fds[0].events = ZSOCK_POLLIN;
  nfds = 1;
}

static void clear_fds(void) { nfds = 0; }

static int poll_mqtt_socket(struct mqtt_client *client, int timeout) {
static int poll_mqtt_socket(struct mqtt_client *client, int timeout) {
  prepare_fds(client);
  if (nfds <= 0) return -EINVAL;
  int rc = zsock_poll(fds, nfds, timeout);
  if (rc < 0) LOG_ERR("Socket poll error [%d]", rc);
  return rc;
}

static void mqtt_event_handler(struct mqtt_client *client, const struct mqtt_evt *evt) {
  int ret;
  switch (evt->type) {
  case MQTT_EVT_CONNACK:
    if (evt->result != 0) {
    if (evt->result != 0) {
      LOG_ERR("MQTT connection failed [%d]", evt->result);
    } else {
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
  case MQTT_EVT_PUBLISH: {
    const struct mqtt_publish_param *pub = &evt->param.publish;
    char start_topic[128], stop_topic[128], registry_topic[128];
    extern const char *channel_id; extern const char *domain_id;

    snprintf(start_topic, sizeof(start_topic), START_TOPIC_TEMPLATE, domain_id, channel_id);
    snprintf(stop_topic, sizeof(stop_topic), STOP_TOPIC_TEMPLATE, domain_id, channel_id);
    snprintf(registry_topic, sizeof(registry_topic), REGISTRY_RESPONSE_TOPIC, domain_id, channel_id);

    size_t len = pub->message.payload.len;
    char *payload = malloc(len + 1);
    if (!payload) {
        LOG_ERR("OOM: Payload buffer (%zu bytes)", len);
        return;
    }

    ret = mqtt_read_publish_payload(&client_ctx, payload, len);
    if (ret < 0) {
      LOG_ERR("Failed to read payload [%d]", ret);
      free(payload);
      return;
    }
    payload[ret] = '\0';

    if (strncmp(pub->message.topic.topic.utf8, start_topic, pub->message.topic.topic.size) == 0) {
      handle_start_command(payload);
    } else if (strncmp(pub->message.topic.topic.utf8, stop_topic, pub->message.topic.topic.size) == 0) {
      handle_stop_command(payload);
    } else if (strncmp(pub->message.topic.topic.utf8, registry_topic, pub->message.topic.topic.size) == 0) {
      handle_registry_response(payload);
    } else {
      LOG_WRN("Unknown topic received");
    }
    free(payload);
    break;
  }

  case MQTT_EVT_PUBREC: {
      const struct mqtt_pubrec_param *param = &evt->param.pubrec;
      ret = mqtt_publish_qos2_release(client, param->message_id);
      if (ret != 0) LOG_ERR("Failed to send PUBREL: %d", ret);
      break;
  }

  case MQTT_EVT_PUBREL: {
      const struct mqtt_pubrel_param *param = &evt->param.pubrel;
      ret = mqtt_publish_qos2_complete(client, param->message_id);
      if (ret != 0) LOG_ERR("Failed to send PUBCOMP: %d", ret);
      break;
  }

  case MQTT_EVT_PUBCOMP: LOG_INF("QoS 2 publish complete"); break;
  case MQTT_EVT_SUBACK: LOG_INF("Subscribed successfully"); break;
  case MQTT_EVT_PINGRESP: break;
  default: break;
  }
}

static void prepare_publish_param(struct mqtt_publish_param *param, const char *topic_str, const char *payload) {
  memset(param, 0, sizeof(*param));
  param->message.topic.topic.utf8 = topic_str;
  param->message.topic.topic.size = strlen(topic_str);
  param->message.topic.qos = MQTT_QOS_1_AT_LEAST_ONCE;
  param->message.payload.data = (uint8_t *)payload;
  param->message.payload.len = strlen(payload);
  param->message_id = sys_rand32_get() & 0xFFFF;
}

int publish(const char *domain_id, const char *channel_id, const char *topic_template, const char *payload) {
  if (!mqtt_connected) return -ENOTCONN;
  char topic[128];
  snprintf(topic, sizeof(topic), topic_template, domain_id, channel_id);
  struct mqtt_publish_param param;
  prepare_publish_param(&param, topic, payload);
  return mqtt_publish(&client_ctx, &param);
}

int mqtt_client_connect(const char *domain_id, const char *proplet_id, const char *channel_id) {
  int ret;

  ret = tls_credential_add(TLS_SEC_TAG, TLS_CREDENTIAL_CA_CERTIFICATE, ca_certificate, sizeof(ca_certificate));
  if (ret < 0) LOG_ERR("Failed to add TLS credentials: %d", ret);

  struct sockaddr_in *broker = (struct sockaddr_in *)&broker_addr;
  broker->sin_family = AF_INET;
  broker->sin_port = htons(MQTT_BROKER_PORT);
  
  ret = net_addr_pton(AF_INET, MQTT_BROKER_HOSTNAME, &broker->sin_addr);
  if (ret != 0) return ret;

  mqtt_client_init(&client_ctx);

  static char will_topic_str[128];
  static char will_message_str[256];
  snprintf(will_topic_str, sizeof(will_topic_str), ALIVE_TOPIC_TEMPLATE, domain_id, channel_id);
  snprintf(will_message_str, sizeof(will_message_str), WILL_MESSAGE_TEMPLATE, proplet_id, K8S_NAMESPACE);

  static struct mqtt_utf8 will_msg = {0};
  will_msg.utf8 = (const uint8_t *)will_message_str;
  will_msg.size = strlen(will_message_str);

  static struct mqtt_topic will_topic = {0};
  will_topic.topic.utf8 = (const uint8_t *)will_topic_str;
  will_topic.topic.size = strlen(will_topic_str);
  will_topic.qos = WILL_QOS;

  client_ctx.broker = &broker_addr;
  client_ctx.evt_cb = mqtt_event_handler;
  client_ctx.client_id = MQTT_UTF8_LITERAL(CLIENT_ID);
  client_ctx.password = &MQTT_UTF8_LITERAL(PROPLET_PASSWORD);
  client_ctx.user_name = &MQTT_UTF8_LITERAL(PROPLET_ID);
  client_ctx.protocol_version = MQTT_VERSION_3_1_1;
  client_ctx.rx_buf = rx_buffer;
  client_ctx.rx_buf_size = RX_BUFFER_SIZE;
  client_ctx.tx_buf = tx_buffer;
  client_ctx.tx_buf_size = TX_BUFFER_SIZE;
  
  client_ctx.will_topic = &will_topic;
  client_ctx.will_message = &will_msg;
  client_ctx.will_retain = WILL_RETAIN;

  client_ctx.transport.type = MQTT_TRANSPORT_SECURE;
  struct mqtt_sec_config *tls = &client_ctx.transport.tls.config;
  tls->peer_verify = TLS_PEER_VERIFY_REQUIRED;
  tls->sec_tag_list = m_sec_tags;
  tls->sec_tag_count = ARRAY_SIZE(m_sec_tags);
  tls->hostname = MQTT_BROKER_HOSTNAME;

  while (!mqtt_connected) {
    LOG_INF("Connecting to MQTT (TLS)...");
    ret = mqtt_connect(&client_ctx);
    if (ret != 0) {
      k_sleep(K_SECONDS(5));
      continue;
    }
    poll_mqtt_socket(&client_ctx, 5000);
    mqtt_input(&client_ctx);
    if (!mqtt_connected) mqtt_abort(&client_ctx);
  }

  LOG_INF("MQTT connected.");
  return 0;
}

int subscribe(const char *domain_id, const char *channel_id) {
  char t1[128], t2[128], t3[128];
  snprintf(t1, sizeof(t1), START_TOPIC_TEMPLATE, domain_id, channel_id);
  snprintf(t2, sizeof(t2), STOP_TOPIC_TEMPLATE, domain_id, channel_id);
  snprintf(t3, sizeof(t3), REGISTRY_RESPONSE_TOPIC, domain_id, channel_id);

  struct mqtt_topic topics[] = {
      { .topic = { .utf8 = t1, .size = strlen(t1) }, .qos = MQTT_QOS_1_AT_LEAST_ONCE },
      { .topic = { .utf8 = t2, .size = strlen(t2) }, .qos = MQTT_QOS_1_AT_LEAST_ONCE },
      { .topic = { .utf8 = t3, .size = strlen(t3) }, .qos = MQTT_QOS_1_AT_LEAST_ONCE },
  };
  struct mqtt_subscription_list list = { .list = topics, .list_count = ARRAY_SIZE(topics), .message_id = 1 };
  
  return mqtt_subscribe(&client_ctx, &list);
}

void handle_start_command(const char *payload) {
void handle_start_command(const char *payload) {
  cJSON *json = cJSON_Parse(payload);
  if (!json) return;

  struct task t = {0};
  char checksum_hex[65] = {0};

  cJSON *id = cJSON_GetObjectItem(json, "id");
  cJSON *name = cJSON_GetObjectItem(json, "name");
  cJSON *url = cJSON_GetObjectItem(json, "image_url");
  cJSON *file = cJSON_GetObjectItem(json, "file");
  cJSON *sum = cJSON_GetObjectItem(json, "checksum");
  cJSON *inputs = cJSON_GetObjectItem(json, "inputs");

  if (!cJSON_IsString(id) || !cJSON_IsString(name)) { cJSON_Delete(json); return; }
  
  strncpy(t.id, id->valuestring, MAX_ID_LEN-1);
  strncpy(t.name, name->valuestring, MAX_NAME_LEN-1);
  if (cJSON_IsString(url)) strncpy(t.image_url, url->valuestring, MAX_URL_LEN-1);
  if (cJSON_IsString(sum)) strncpy(checksum_hex, sum->valuestring, 64);
  if (cJSON_IsArray(inputs)) {
      int cnt = cJSON_GetArraySize(inputs);
      t.inputs_count = (cnt > MAX_INPUTS) ? MAX_INPUTS : cnt;
      for(int i=0; i<t.inputs_count; i++) {
          cJSON *n = cJSON_GetArrayItem(inputs, i);
          if(cJSON_IsNumber(n)) t.inputs[i] = (uint64_t)n->valuedouble;
      }
  }

  LOG_INF("Start task: %s", t.name);

  if (cJSON_IsString(file) && strlen(file->valuestring) > 0) {
      size_t b64_len = strlen(file->valuestring);
      if ((b64_len * 3 / 4) > MAX_WASM_FILE_SIZE) {
          LOG_ERR("File too large"); cJSON_Delete(json); return;
      }
      
      uint8_t *enc = malloc((b64_len * 3) / 4);
      if (!enc) { cJSON_Delete(json); return; }

      size_t enc_len;
      if (base64_decode(enc, (b64_len * 3) / 4, &enc_len, (const uint8_t*)file->valuestring, b64_len) < 0) {
          free(enc); cJSON_Delete(json); return;
      }

      if (verify_checksum(enc, enc_len, checksum_hex) != 0) {
          free(enc); cJSON_Delete(json); return;
      }

      uint8_t *dec = malloc(enc_len);
      if (!dec) { free(enc); cJSON_Delete(json); return; }

      size_t dec_len;
      if (decrypt_payload(enc, enc_len, workload_key, dec, &dec_len) == 0) {
          persist_workload(t.id, dec, dec_len);
          g_current_task.file_len = dec_len;
          execute_wasm_module(t.id, dec, dec_len, t.inputs, t.inputs_count);
      }
      free(enc); free(dec);
  } 
  else if (strlen(t.image_url) > 0) {
      extern const char *channel_id; extern const char *domain_id;
      publish_registry_request(domain_id, channel_id, t.image_url);
  }

  memcpy(&g_current_task, &t, sizeof(t));
  cJSON_Delete(json);
}

void handle_stop_command(const char *payload) {
void handle_stop_command(const char *payload) {
  cJSON *json = cJSON_Parse(payload);
  if (!json) return;

  cJSON *id = cJSON_GetObjectItem(json, "id");
  if (cJSON_IsString(id)) {
      if (strcmp(id->valuestring, g_current_task.id) == 0) {
          LOG_INF("Stopping task: %s", id->valuestring);
          stop_wasm_app(id->valuestring);
      } else {
          LOG_WRN("Ignored STOP (ID mismatch)");
      }
  }
  cJSON_Delete(json);
}

int handle_registry_response(const char *payload) {
  cJSON *json = cJSON_Parse(payload);
  if (!json) return -1;

  cJSON *idx_j = cJSON_GetObjectItem(json, "chunk_idx");
  cJSON *tot_j = cJSON_GetObjectItem(json, "total_chunks");
  cJSON *dat_j = cJSON_GetObjectItem(json, "data");
  cJSON *sum_j = cJSON_GetObjectItem(json, "checksum");

  if (!cJSON_IsNumber(idx_j) || !cJSON_IsNumber(tot_j) || !cJSON_IsString(dat_j)) {
      cJSON_Delete(json); return -1;
  }

  int idx = idx_j->valueint;
  int total = tot_j->valueint;

  if (idx == 0) {
      if (assembly_buffer) free(assembly_buffer);
      assembly_buffer = malloc(MAX_WASM_FILE_SIZE);
      if (!assembly_buffer) {
          LOG_ERR("OOM Assembly"); cJSON_Delete(json); return -1;
      }
      assembly_cursor = 0;
      expected_chunk_idx = 0;
      if (cJSON_IsString(sum_j)) strncpy(expected_checksum, sum_j->valuestring, 64);
  }

  if (idx != expected_chunk_idx) {
      LOG_ERR("Chunk loss %d vs %d", idx, expected_chunk_idx);
      free(assembly_buffer); assembly_buffer = NULL;
      cJSON_Delete(json); return -1;
  }

  size_t len;
  if (base64_decode(assembly_buffer + assembly_cursor, MAX_WASM_FILE_SIZE - assembly_cursor, 
                    &len, (const uint8_t*)dat_j->valuestring, strlen(dat_j->valuestring)) < 0) {
      free(assembly_buffer); assembly_buffer = NULL;
      cJSON_Delete(json); return -1;
  }

  assembly_cursor += len;
  expected_chunk_idx++;

  if (idx + 1 == total) {
      LOG_INF("Reassembly done. Verifying...");
      
      if (verify_checksum(assembly_buffer, assembly_cursor, expected_checksum) != 0) {
          free(assembly_buffer); assembly_buffer = NULL;
          cJSON_Delete(json); return -1;
      }

      uint8_t *dec = malloc(assembly_cursor);
      if (!dec) { free(assembly_buffer); assembly_buffer = NULL; cJSON_Delete(json); return -1; }

      size_t dec_len;
      if (decrypt_payload(assembly_buffer, assembly_cursor, workload_key, dec, &dec_len) == 0) {
          extern const char *channel_id; extern const char *domain_id;
          char topic[128], msg[128];
          snprintf(topic, sizeof(topic), REGISTRY_ACK_TOPIC_TEMPLATE, domain_id, channel_id);
          snprintf(msg, sizeof(msg), "{\"id\":\"%s\",\"status\":\"downloaded\"}", g_current_task.id);
          publish(domain_id, channel_id, REGISTRY_ACK_TOPIC_TEMPLATE, msg);

          persist_workload(g_current_task.id, dec, dec_len);
          execute_wasm_module(g_current_task.id, dec, dec_len, g_current_task.inputs, g_current_task.inputs_count);
      }
      free(assembly_buffer); assembly_buffer = NULL;
      free(dec);
  }

  cJSON_Delete(json);
  return 0;
}

void publish_alive_message(const char *domain_id, const char *channel_id) {
void publish_alive_message(const char *domain_id, const char *channel_id) {
  char payload[128];
  snprintf(payload, sizeof(payload), "{\"status\":\"alive\",\"proplet_id\":\"%s\",\"namespace\":\"%s\"}", CLIENT_ID, K8S_NAMESPACE);
  publish(domain_id, channel_id, ALIVE_TOPIC_TEMPLATE, payload);
}

int publish_discovery(const char *domain_id, const char *proplet_id, const char *channel_id) {
  if (!mqtt_connected) return -ENOTCONN;
  char topic[128], payload[128];
  snprintf(topic, sizeof(topic), DISCOVERY_TOPIC_TEMPLATE, domain_id, channel_id);
  snprintf(payload, sizeof(payload), "{\"proplet_id\":\"%s\"}", proplet_id);
  struct mqtt_publish_param param;
  prepare_publish_param(&param, topic, payload);
  return mqtt_publish(&client_ctx, &param);
}

void publish_registry_request(const char *domain_id, const char *channel_id, const char *app_name) {
  char payload[128];
  snprintf(payload, sizeof(payload), "{\"app_name\":\"%s\"}", app_name);
  publish(domain_id, channel_id, FETCH_REQUEST_TOPIC_TEMPLATE, payload);
}

void publish_results(const char *domain_id, const char *channel_id, const char *task_id, const char *results) {
  char payload[256];
  snprintf(payload, sizeof(payload), "{\"task_id\":\"%s\",\"results\":\"%s\"}", task_id, results);
  publish(domain_id, channel_id, RESULTS_TOPIC_TEMPLATE, payload);
}

void mqtt_client_process(void) {
  if (mqtt_connected) {
    int ret = poll_mqtt_socket(&client_ctx, mqtt_keepalive_time_left(&client_ctx));
    if (ret > 0) mqtt_input(&client_ctx);
    mqtt_live(&client_ctx);
  }
}
