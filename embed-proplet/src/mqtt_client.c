#include <zephyr/data/json.h>
#include "mqtt_client.h"
#include <string.h>
#include <stdlib.h>
#include <zephyr/logging/log.h>
#include <zephyr/net/socket.h>
#include <zephyr/kernel.h>
#include <zephyr/random/random.h>
#include <zephyr/sys/base64.h>
#include <zephyr/storage/disk_access.h>
#include "wasm_handler.h"

LOG_MODULE_REGISTER(mqtt_client);

#define RX_BUFFER_SIZE 256
#define TX_BUFFER_SIZE 256

#define MQTT_BROKER_HOSTNAME "192.168.88.179" /* Replace with your broker's IP */
#define MQTT_BROKER_PORT     1883

#define REGISTRY_ACK_TOPIC_TEMPLATE  "channels/%s/messages/control/manager/registry"
#define ALIVE_TOPIC_TEMPLATE         "channels/%s/messages/control/proplet/alive"
#define DISCOVERY_TOPIC_TEMPLATE     "channels/%s/messages/control/proplet/create"
#define START_TOPIC_TEMPLATE         "channels/%s/messages/control/manager/start"
#define STOP_TOPIC_TEMPLATE          "channels/%s/messages/control/manager/stop"
#define REGISTRY_RESPONSE_TOPIC      "channels/%s/messages/registry/server"
#define FETCH_REQUEST_TOPIC_TEMPLATE "channels/%s/messages/registry/proplet"
#define RESULTS_TOPIC_TEMPLATE       "channels/%s/messages/control/proplet/results"

#define WILL_MESSAGE_TEMPLATE "{\"status\":\"offline\",\"proplet_id\":\"%s\",\"mg_channel_id\":\"%s\"}"
#define WILL_QOS              MQTT_QOS_1_AT_LEAST_ONCE
#define WILL_RETAIN           1

#define CLIENT_ID "proplet-esp32s3"

#define MAX_ID_LEN         64
#define MAX_NAME_LEN       64
#define MAX_STATE_LEN      16
#define MAX_URL_LEN        256
#define MAX_TIMESTAMP_LEN  32
#define MAX_BASE64_LEN     512
#define MAX_INPUTS         16
#define MAX_RESULTS        16

/* 
 * Keep the most recent "start" Task here, so if 
 * we fetch the WASM from the registry, we can call 
 * the WASM with the same inputs. 
 *
 * If you support multiple tasks in parallel, you'll need
 * a more robust approach than a single global.
 */
static struct task g_current_task;

static uint8_t rx_buffer[RX_BUFFER_SIZE];
static uint8_t tx_buffer[TX_BUFFER_SIZE];

static struct mqtt_client client_ctx;
static struct sockaddr_storage broker_addr;

static struct zsock_pollfd fds[1];
static int nfds;

bool mqtt_connected = false;

struct task {
    char     id[MAX_ID_LEN];
    char     name[MAX_NAME_LEN];
    char     state[MAX_STATE_LEN];
    char     image_url[MAX_URL_LEN];

    char     file[MAX_BASE64_LEN];  
    size_t   file_len;     

    uint64_t inputs[MAX_INPUTS];
    size_t   inputs_count;
    uint64_t results[MAX_RESULTS];
    size_t   results_count;

    char     start_time[MAX_TIMESTAMP_LEN];
    char     finish_time[MAX_TIMESTAMP_LEN];
    char     created_at[MAX_TIMESTAMP_LEN];
    char     updated_at[MAX_TIMESTAMP_LEN];
};

struct registry_response {
    char app_name[64];
    char data[MAX_BASE64_LEN];
};

static const struct json_obj_descr task_descr[] = {
    JSON_OBJ_DESCR_PRIM_NAMED(struct task, "id",         id,        JSON_TOK_STRING),
    JSON_OBJ_DESCR_PRIM_NAMED(struct task, "name",       name,      JSON_TOK_STRING),
    JSON_OBJ_DESCR_PRIM_NAMED(struct task, "state",      state,     JSON_TOK_STRING),
    JSON_OBJ_DESCR_PRIM_NAMED(struct task, "image_url",  image_url, JSON_TOK_STRING),

    JSON_OBJ_DESCR_PRIM_NAMED(struct task, "file",       file,      JSON_TOK_STRING),

    JSON_OBJ_DESCR_ARRAY_NAMED(struct task, "inputs",    inputs,  MAX_INPUTS,  inputs_count,  JSON_TOK_NUMBER),
};

static const struct json_obj_descr registry_response_descr[] = {
    JSON_OBJ_DESCR_PRIM_NAMED(struct registry_response, "app_name", app_name, JSON_TOK_STRING),
    JSON_OBJ_DESCR_PRIM_NAMED(struct registry_response, "data",     data,     JSON_TOK_STRING),
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

static void clear_fds(void)
{
    nfds = 0;
}

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

static void mqtt_event_handler(struct mqtt_client *client, const struct mqtt_evt *evt)
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

        char start_topic[128];
        char stop_topic[128];
        char registry_response_topic[128];

        snprintf(start_topic,              sizeof(start_topic),              START_TOPIC_TEMPLATE,        channel_id);
        snprintf(stop_topic,               sizeof(stop_topic),               STOP_TOPIC_TEMPLATE,         channel_id);
        snprintf(registry_response_topic,  sizeof(registry_response_topic),  REGISTRY_RESPONSE_TOPIC,     channel_id);

        LOG_INF("Message received on topic: %s", pub->message.topic.topic.utf8);

        ret = mqtt_read_publish_payload(
            &client_ctx,
            payload,
            MIN(pub->message.payload.len, PAYLOAD_BUFFER_SIZE - 1)
        );
        if (ret < 0) {
            LOG_ERR("Failed to read payload [%d]", ret);
            return;
        }
        payload[ret] = '\0'; /* Null-terminate */
        LOG_INF("Payload: %s", payload);

        if (strncmp(pub->message.topic.topic.utf8, start_topic, pub->message.topic.topic.size) == 0) {
            handle_start_command(payload);
        }
        else if (strncmp(pub->message.topic.topic.utf8, stop_topic, pub->message.topic.topic.size) == 0) {
            handle_stop_command(payload);
        }
        else if (strncmp(pub->message.topic.topic.utf8, registry_response_topic, pub->message.topic.topic.size) == 0) {
            handle_registry_response(payload);
        }
        else {
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
    param->message.topic.qos        = MQTT_QOS_1_AT_LEAST_ONCE;

    param->message.payload.data = (uint8_t *)payload;
    param->message.payload.len  = strlen(payload);

    param->message_id = sys_rand32_get() & 0xFFFF;
    param->dup_flag   = 0;
    param->retain_flag= 0;
}

int publish(const char *channel_id, const char *topic_template, const char *payload)
{
    if (!mqtt_connected) {
        LOG_ERR("MQTT client is not connected. Cannot publish.");
        return -ENOTCONN;
    }

    char topic[128];
    snprintf(topic, sizeof(topic), topic_template, channel_id);

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

int mqtt_client_connect(const char *proplet_id, const char *channel_id)
{
    int ret;
    struct sockaddr_in *broker = (struct sockaddr_in *)&broker_addr;

    broker->sin_family = AF_INET;
    broker->sin_port   = htons(MQTT_BROKER_PORT);

    ret = net_addr_pton(AF_INET, MQTT_BROKER_HOSTNAME, &broker->sin_addr);
    if (ret != 0) {
        LOG_ERR("Failed to resolve broker address, ret=%d", ret);
        return ret;
    }

    mqtt_client_init(&client_ctx);

    char will_topic_str[128];
    snprintf(will_topic_str, sizeof(will_topic_str), ALIVE_TOPIC_TEMPLATE, channel_id);

    char will_message_str[256];
    snprintf(will_message_str, sizeof(will_message_str),
             WILL_MESSAGE_TEMPLATE, proplet_id, channel_id);

    struct mqtt_utf8 will_message = {
        .utf8 = (const uint8_t *)will_message_str,
        .size = strlen(will_message_str),
    };

    struct mqtt_topic will_topic = {
        .topic = {
            .utf8 = (const uint8_t *)will_topic_str,
            .size = strlen(will_topic_str),
        },
        .qos = WILL_QOS,
    };

    client_ctx.broker          = &broker_addr;
    client_ctx.evt_cb          = mqtt_event_handler;
    client_ctx.client_id.utf8  = CLIENT_ID;
    client_ctx.client_id.size  = strlen(CLIENT_ID);
    client_ctx.protocol_version= MQTT_VERSION_3_1_1;
    client_ctx.transport.type  = MQTT_TRANSPORT_NON_SECURE;

    client_ctx.rx_buf          = rx_buffer;
    client_ctx.rx_buf_size     = RX_BUFFER_SIZE;
    client_ctx.tx_buf          = tx_buffer;
    client_ctx.tx_buf_size     = TX_BUFFER_SIZE;

    client_ctx.will_topic      = &will_topic;
    client_ctx.will_message    = &will_message;
    client_ctx.will_retain     = WILL_RETAIN;

    while (!mqtt_connected) {
        LOG_INF("Attempting to connect to the MQTT broker...");

        ret = mqtt_connect(&client_ctx);
        if (ret != 0) {
            LOG_ERR("MQTT connect failed [%d]. Retrying in 5 seconds...", ret);
            k_sleep(K_SECONDS(5));
            continue;
        }

        /* Poll the socket for a response */
        ret = poll_mqtt_socket(&client_ctx, 5000);
        if (ret < 0) {
            LOG_ERR("Socket poll failed, ret=%d. Retrying in 5 seconds...", ret);
            mqtt_abort(&client_ctx);
            k_sleep(K_SECONDS(5));
            continue;
        }
        else if (ret == 0) {
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

int subscribe(const char *channel_id)
{
    char start_topic[128];
    char stop_topic[128];
    char registry_response_topic[128];

    snprintf(start_topic,             sizeof(start_topic),             START_TOPIC_TEMPLATE,        channel_id);
    snprintf(stop_topic,              sizeof(stop_topic),              STOP_TOPIC_TEMPLATE,         channel_id);
    snprintf(registry_response_topic, sizeof(registry_response_topic), REGISTRY_RESPONSE_TOPIC,     channel_id);

    struct mqtt_topic topics[] = {
        {
            .topic = {
                .utf8 = start_topic,
                .size = strlen(start_topic),
            },
            .qos = MQTT_QOS_1_AT_LEAST_ONCE,
        },
        {
            .topic = {
                .utf8 = stop_topic,
                .size = strlen(stop_topic),
            },
            .qos = MQTT_QOS_1_AT_LEAST_ONCE,
        },
        {
            .topic = {
                .utf8 = registry_response_topic,
                .size = strlen(registry_response_topic),
            },
            .qos = MQTT_QOS_1_AT_LEAST_ONCE,
        },
    };

    struct mqtt_subscription_list sub_list = {
        .list       = topics,
        .list_count = ARRAY_SIZE(topics),
        .message_id = 1,
    };

    LOG_INF("Subscribing to topics for channel ID: %s", channel_id);

    int ret = mqtt_subscribe(&client_ctx, &sub_list);
    if (ret != 0) {
        LOG_ERR("Failed to subscribe to topics for channel ID: %s. Error: %d", channel_id, ret);
    } else {
        LOG_INF("Successfully subscribed to topics for channel ID: %s", channel_id);
    }

    return ret;
}

void handle_start_command(const char *payload)
{
    struct task t;
    memset(&t, 0, sizeof(t));

    int ret = json_obj_parse(payload, strlen(payload),
                             task_descr,
                             ARRAY_SIZE(task_descr),
                             &t);
    if (ret < 0) {
        LOG_ERR("Failed to parse START task payload, error: %d", ret);
        return;
    }

    char encoded_json[1024]; // Adjust size based on expected payload length
    ret = json_obj_encode_buf(task_descr, ARRAY_SIZE(task_descr), &t, encoded_json, sizeof(encoded_json));
    if (ret < 0) {
        LOG_ERR("Failed to encode struct to JSON, error: %d", ret);
    } else {
        LOG_INF("Encoded JSON: %s", encoded_json);
    }

    if (strlen(t.id) == 0 || strlen(t.name) == 0 || strlen(t.state) == 0) {
        LOG_ERR("Parsed task contains invalid or missing mandatory fields.");
        return;
    }

    t.id[MAX_ID_LEN - 1] = '\0';
    t.name[MAX_NAME_LEN - 1] = '\0';
    t.state[MAX_STATE_LEN - 1] = '\0';

    LOG_INF("Starting task: ID=%.63s, Name=%.63s, State=%.15s", t.id, t.name, t.state);

    if (strlen(t.file) > 0) {
        size_t required_size = 0;

        // Calculate required buffer size
        ret = base64_decode(NULL, 0, &required_size, (const uint8_t *)t.file, strlen(t.file));
        if (ret < 0) {
            LOG_ERR("Failed to calculate buffer size for base64 decode, err=%d", ret);
            return;
        }

        if (required_size > MAX_BASE64_LEN) {
            LOG_ERR("Decoded size exceeds buffer capacity");
            return;
        }

        static uint8_t wasm_binary[MAX_BASE64_LEN];
        size_t wasm_decoded_len = 0;

        // Perform actual decoding
        ret = base64_decode(wasm_binary, sizeof(wasm_binary), &wasm_decoded_len,
                            (const uint8_t *)t.file, strlen(t.file));
        if (ret < 0) {
            LOG_ERR("Failed to decode base64 WASM (task.file). Err=%d", ret);
            return;
        }

        g_current_task.file_len = wasm_decoded_len;

        execute_wasm_module(g_current_task.id,
                            wasm_binary,
                            g_current_task.file_len,
                            g_current_task.inputs,
                            g_current_task.inputs_count);
    }
    else if (strlen(t.image_url) > 0) {
        LOG_INF("Requesting WASM from registry: %s", t.image_url);
        extern const char *channel_id;
        publish_registry_request(channel_id, t.image_url);
    }
    else {
        LOG_WRN("No file or image_url specified; cannot start WASM task!");
    }

    memcpy(&g_current_task, &t, sizeof(t));
}

void handle_stop_command(const char *payload)
{
    struct task t;
    memset(&t, 0, sizeof(t));

    int ret = json_obj_parse(payload, strlen(payload),
                             task_descr,
                             ARRAY_SIZE(task_descr),
                             &t);
    if (ret < 0) {
        LOG_ERR("Failed to parse STOP task payload, error: %d", ret);
        return;
    }

    LOG_INF("Stopping task: ID=%s, Name=%s, State=%s", t.id, t.name, t.state);
    stop_wasm_app(t.id);
}

/**
 *   We receive a single chunk "data" field with the full base64 WASM.
 */
int handle_registry_response(const char *payload)
{
    struct registry_response resp;
    memset(&resp, 0, sizeof(resp));

    int ret = json_obj_parse(payload, strlen(payload),
                             registry_response_descr,
                             ARRAY_SIZE(registry_response_descr),
                             &resp);
    if (ret < 0) {
        LOG_ERR("Failed to parse registry response, error: %d", ret);
        return -1;
    }

    LOG_INF("Single-chunk registry response for app: %s", resp.app_name);

    size_t encoded_len = strlen(resp.data);
    size_t decoded_len = (encoded_len * 3) / 4;
    uint8_t *binary_data = malloc(decoded_len);
    if (!binary_data) {
        LOG_ERR("Failed to allocate memory for decoded binary");
        return -1;
    }

    size_t actual_decoded_len = decoded_len;
    ret = base64_decode(binary_data,
                        decoded_len,
                        &actual_decoded_len,
                        (const uint8_t *)resp.data,
                        encoded_len);
    if (ret < 0) {
        LOG_ERR("Failed to decode base64 single-chunk, err=%d", ret);
        free(binary_data);
        return -1;
    }

    LOG_INF("Decoded single-chunk WASM size: %zu. Executing now...", actual_decoded_len);

    execute_wasm_module(g_current_task.id,
                        binary_data,
                        actual_decoded_len,
                        g_current_task.inputs,
                        g_current_task.inputs_count);

    free(binary_data);

    LOG_INF("WASM binary executed from single-chunk registry response.");
    return 0;
}

void publish_alive_message(const char *channel_id)
{
    char payload[128];
    snprintf(payload, sizeof(payload),
             "{\"status\":\"alive\",\"proplet_id\":\"%s\",\"mg_channel_id\":\"%s\"}",
             CLIENT_ID, channel_id);
    publish(channel_id, ALIVE_TOPIC_TEMPLATE, payload);
}

int publish_discovery(const char *proplet_id, const char *channel_id)
{
    char topic[128];
    char payload[128];

    snprintf(topic,   sizeof(topic),   DISCOVERY_TOPIC_TEMPLATE, channel_id);
    snprintf(payload, sizeof(payload),
             "{\"proplet_id\":\"%s\",\"mg_channel_id\":\"%s\"}",
             proplet_id, channel_id);

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

void publish_registry_request(const char *channel_id, const char *app_name)
{
    char payload[128];
    snprintf(payload, sizeof(payload), "{\"app_name\":\"%s\"}", app_name);

    if (publish(channel_id, FETCH_REQUEST_TOPIC_TEMPLATE, payload) != 0) {
        LOG_ERR("Failed to request registry file");
    } else {
        LOG_INF("Requested registry file for: %s", app_name);
    }
}

void publish_results(const char *channel_id, const char *task_id, const char *results)
{
    char results_payload[256];

    snprintf(results_payload, sizeof(results_payload),
             "{\"task_id\":\"%s\",\"results\":\"%s\"}",
             task_id, results);

    if (publish(channel_id, RESULTS_TOPIC_TEMPLATE, results_payload) != 0) {
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
