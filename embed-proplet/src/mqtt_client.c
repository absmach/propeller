#include <zephyr/data/json.h>
#include "mqtt_client.h"
#include <string.h>
#include <stdlib.h>
#include <zephyr/logging/log.h>
#include <zephyr/net/socket.h>
#include <zephyr/kernel.h>
#include <zephyr/random/random.h>
#include <zephyr/sys/base64.h>
#include <zephyr/fs/fs.h>
#include <zephyr/storage/disk_access.h>

LOG_MODULE_REGISTER(mqtt_client);

#define RX_BUFFER_SIZE 256
#define TX_BUFFER_SIZE 256

#define MQTT_BROKER_HOSTNAME "192.168.88.179" /* Replace with your broker's IP */
#define MQTT_BROKER_PORT 1883

#define REGISTRY_ACK_TOPIC_TEMPLATE "channels/%s/messages/control/manager/registry"
#define ALIVE_TOPIC_TEMPLATE        "channels/%s/messages/control/proplet/alive"
#define DISCOVERY_TOPIC_TEMPLATE    "channels/%s/messages/control/proplet/create"
#define START_TOPIC_TEMPLATE        "channels/%s/messages/control/manager/start"
#define STOP_TOPIC_TEMPLATE         "channels/%s/messages/control/manager/stop"
#define REGISTRY_RESPONSE_TOPIC     "channels/%s/messages/registry/server"
#define FETCH_REQUEST_TOPIC_TEMPLATE "channels/%s/messages/registry/proplet"
#define RESULTS_TOPIC_TEMPLATE      "channels/%s/messages/control/proplet/results"

#define CLIENT_ID "proplet-esp32s3"

/* Buffers for MQTT client */
static uint8_t rx_buffer[RX_BUFFER_SIZE];
static uint8_t tx_buffer[TX_BUFFER_SIZE];

/* MQTT client context */
static struct mqtt_client client_ctx;
static struct sockaddr_storage broker_addr;

/* Socket descriptor */
static struct zsock_pollfd fds[1];
static int nfds;

bool mqtt_connected = false;
struct start_command {
    char task_id[50];
    char function_name[50];
    char wasm_file[100];
};

struct stop_command {
    char task_id[50];
};

struct registry_response {
    char app_name[50];
    int chunk_idx;
    int total_chunks;
    char data[256];
};

struct chunk_tracker {
    int total_chunks;
    int received_chunks;
    bool *chunk_received;
    uint8_t *file_data;
};

static const struct json_obj_descr start_command_descr[] = {
    JSON_OBJ_DESCR_PRIM(struct start_command, task_id, JSON_TOK_STRING),
    JSON_OBJ_DESCR_PRIM(struct start_command, function_name, JSON_TOK_STRING),
    JSON_OBJ_DESCR_PRIM(struct start_command, wasm_file, JSON_TOK_STRING),
};

static const struct json_obj_descr stop_command_descr[] = {
    JSON_OBJ_DESCR_PRIM(struct stop_command, task_id, JSON_TOK_STRING),
};

static const struct json_obj_descr registry_response_descr[] = {
    JSON_OBJ_DESCR_PRIM(struct registry_response, app_name, JSON_TOK_STRING),
    JSON_OBJ_DESCR_PRIM(struct registry_response, chunk_idx, JSON_TOK_NUMBER),
    JSON_OBJ_DESCR_PRIM(struct registry_response, total_chunks, JSON_TOK_NUMBER),
    JSON_OBJ_DESCR_PRIM(struct registry_response, data, JSON_TOK_STRING),
};

static struct chunk_tracker *app_chunk_tracker = NULL;

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

    case MQTT_EVT_PUBLISH:
        {
            const struct mqtt_publish_param *pub = &evt->param.publish;
            char payload[PAYLOAD_BUFFER_SIZE];
            char start_topic[128];
            char stop_topic[128];
            char registry_response_topic[128];
            int ret;

            extern const char *channel_id;

            snprintf(start_topic, sizeof(start_topic), START_TOPIC_TEMPLATE, channel_id);
            snprintf(stop_topic, sizeof(stop_topic), STOP_TOPIC_TEMPLATE, channel_id);
            snprintf(registry_response_topic, sizeof(registry_response_topic), REGISTRY_RESPONSE_TOPIC, channel_id);

            LOG_INF("Message received on topic: %s", pub->message.topic.topic.utf8);

            ret = mqtt_read_publish_payload(&client_ctx, payload, MIN(pub->message.payload.len, PAYLOAD_BUFFER_SIZE - 1));
            if (ret < 0) {
                LOG_ERR("Failed to read payload [%d]", ret);
                return;
            }

            // Null-terminate the payload
            payload[ret] = '\0';

            LOG_INF("Payload: %s", payload);

            if (strncmp(pub->message.topic.topic.utf8, start_topic, pub->message.topic.topic.size) == 0) {
                handle_start_command(payload);
            } else if (strncmp(pub->message.topic.topic.utf8, stop_topic, pub->message.topic.topic.size) == 0) {
                handle_stop_command(payload);
            } else if (strncmp(pub->message.topic.topic.utf8, registry_response_topic, pub->message.topic.topic.size) == 0) {
                handle_registry_response(payload);
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

int publish(const char *channel_id, const char *topic_template, const char *payload)
{
    char topic[128];

    snprintf(topic, sizeof(topic), topic_template, channel_id);

    if (!mqtt_connected) {
        LOG_ERR("MQTT client is not connected. Cannot publish to topic: %s", topic);
        return -ENOTCONN;
    }

    struct mqtt_publish_param param = {
        .message = {
            .topic = {
                .topic = {
                    .utf8 = topic,
                    .size = strlen(topic),
                },
                .qos = MQTT_QOS_1_AT_LEAST_ONCE,
            },
            .payload = {
                .data = (uint8_t *)payload,
                .len = strlen(payload),
            },
        },
        .message_id = sys_rand32_get() & 0xFFFF,
        .dup_flag = 0,
        .retain_flag = 0
    };

    int ret = mqtt_publish(&client_ctx, &param);
    if (ret != 0) {
        LOG_ERR("Failed to publish to topic: %s. Error: %d", topic, ret);
        return ret;
    }

    LOG_INF("Published to topic: %s. Payload: %s", topic, payload);
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

void publish_registry_request(const char *channel_id, const char *app_name)
{
    char payload[128];
    snprintf(payload, sizeof(payload), "{\"app_name\":\"%s\"}", app_name);
    publish(channel_id, FETCH_REQUEST_TOPIC_TEMPLATE, payload);
}

int mqtt_client_connect(void)
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
    client_ctx.broker = &broker_addr;
    client_ctx.evt_cb = mqtt_event_handler;
    client_ctx.client_id.utf8 = CLIENT_ID;
    client_ctx.client_id.size = strlen(CLIENT_ID);
    client_ctx.protocol_version = MQTT_VERSION_3_1_1;
    client_ctx.transport.type = MQTT_TRANSPORT_NON_SECURE;
    client_ctx.rx_buf = rx_buffer;
    client_ctx.rx_buf_size = sizeof(rx_buffer);
    client_ctx.tx_buf = tx_buffer;
    client_ctx.tx_buf_size = sizeof(tx_buffer);

    while (!mqtt_connected) {
        ret = mqtt_connect(&client_ctx);
        if (ret != 0) {
            LOG_ERR("MQTT connect failed [%d]. Retrying...", ret);
            k_sleep(K_SECONDS(5));
            continue;
        }

        /* Poll the socket for a response */
        ret = poll_mqtt_socket(&client_ctx, 5000);
        if (ret < 0) {
            LOG_ERR("Socket poll failed, ret=%d", ret);
            mqtt_abort(&client_ctx);
            continue;
        } else if (ret == 0) {
            LOG_ERR("Poll timed out waiting for CONNACK. Retrying...");
            mqtt_abort(&client_ctx);
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

int publish_discovery(const char *proplet_id, const char *channel_id)
{
    char topic[128];
    char payload[128];

    snprintf(topic, sizeof(topic), DISCOVERY_TOPIC_TEMPLATE, channel_id);
    snprintf(payload, sizeof(payload),
             "{\"proplet_id\":\"%s\",\"mg_channel_id\":\"%s\"}",
             proplet_id, channel_id);

    LOG_DBG("Topic: %s (Length: %zu)", topic, strlen(topic));
    LOG_DBG("Payload: %s (Length: %zu)", payload, strlen(payload));

    if (strlen(topic) >= sizeof(topic) || strlen(payload) >= sizeof(payload)) {
        LOG_ERR("Topic or payload size exceeds the maximum allowable size.");
        return -EINVAL;
    }

    if (!mqtt_connected) {
        LOG_ERR("MQTT client is not connected. Discovery announcement aborted.");
        return -ENOTCONN;
    }

    struct mqtt_publish_param param = {
        .message = {
            .topic = {
                .topic = {
                    .utf8 = (uint8_t *)topic,
                    .size = strlen(topic),
                },
                .qos = MQTT_QOS_1_AT_LEAST_ONCE,
            },
            .payload = {
                .data = (uint8_t *)payload,
                .len = strlen(payload),
            },
        },
        .message_id = sys_rand32_get() & 0xFFFF,
        .dup_flag = 0,
        .retain_flag = 0
    };

    int ret = mqtt_publish(&client_ctx, &param);
    if (ret != 0) {
        LOG_ERR("Failed to publish discovery announcement. Error code: %d", ret);
        return ret;
    }

    LOG_INF("Discovery announcement published successfully to topic: %s", topic);
    return 0;
}

int subscribe(const char *channel_id)
{
    char start_topic[128];
    char stop_topic[128];
    char registry_response_topic[128];

    snprintf(start_topic, sizeof(start_topic), START_TOPIC_TEMPLATE, channel_id);
    snprintf(stop_topic, sizeof(stop_topic), STOP_TOPIC_TEMPLATE, channel_id);
    snprintf(registry_response_topic, sizeof(registry_response_topic), REGISTRY_RESPONSE_TOPIC, channel_id);

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
        .list = topics,
        .list_count = ARRAY_SIZE(topics),
        .message_id = 1,
    };

    int ret = mqtt_subscribe(&client_ctx, &sub_list);
    if (ret != 0) {
        LOG_ERR("Failed to subscribe to topics, ret=%d", ret);
    } else {
        LOG_INF("Subscribed to topics successfully");
    }

    return ret;
}


void handle_start_command(const char *payload) {
    struct start_command cmd;
    int ret;

    ret = json_obj_parse((char *)payload, strlen(payload), start_command_descr, ARRAY_SIZE(start_command_descr), &cmd);

    if (ret < 0) {
        LOG_ERR("Failed to parse start command payload, error: %d", ret);
        return;
    }

    LOG_INF("Starting task:");
    LOG_INF("Task ID: %s", cmd.task_id);
    LOG_INF("Function: %s", cmd.function_name);
    LOG_INF("Wasm File: %s", cmd.wasm_file);

    // TODO: Use WAMR runtime to start the task
    // Example:
    // wamr_start_app(cmd.wasm_file, cmd.function_name);
}

void handle_stop_command(const char *payload) {
    struct stop_command cmd;
    int ret;

    ret = json_obj_parse(payload, strlen(payload), stop_command_descr, ARRAY_SIZE(stop_command_descr), &cmd);
    if (ret < 0) {
        LOG_ERR("Failed to parse stop command payload, error: %d", ret);
        return;
    }

    LOG_INF("Stopping task:");
    LOG_INF("Task ID: %s", cmd.task_id);

    // TODO: Use WAMR runtime to stop the task
    // Example:
    // wamr_stop_app(cmd.task_id);
}

void request_registry_file(const char *channel_id, const char *app_name)
{
    char registry_payload[128];

    snprintf(registry_payload, sizeof(registry_payload),
             "{\"app_name\":\"%s\"}",
             app_name);

    if (publish(channel_id, FETCH_REQUEST_TOPIC_TEMPLATE, registry_payload) != 0) {
        LOG_ERR("Failed to request registry file");
    } else {
        LOG_INF("Requested registry file for app: %s", app_name);
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

void handle_registry_response(const char *payload) {
    struct registry_response resp;
    int ret;

    ret = json_obj_parse((char *)payload, strlen(payload), registry_response_descr, ARRAY_SIZE(registry_response_descr), &resp);
    if (ret < 0) {
        LOG_ERR("Failed to parse registry response, error: %d", ret);
        return;
    }

    LOG_INF("Registry response received:");
    LOG_INF("App Name: %s", resp.app_name);
    LOG_INF("Chunk Index: %d", resp.chunk_idx);
    LOG_INF("Total Chunks: %d", resp.total_chunks);

    if (app_chunk_tracker == NULL) {
        app_chunk_tracker = malloc(sizeof(struct chunk_tracker));
        if (!app_chunk_tracker) {
            LOG_ERR("Failed to allocate memory for chunk tracker");
            return;
        }

        app_chunk_tracker->total_chunks = resp.total_chunks;
        app_chunk_tracker->received_chunks = 0;

        app_chunk_tracker->chunk_received = calloc(resp.total_chunks, sizeof(bool));
        app_chunk_tracker->file_data = malloc(resp.total_chunks * 256); // Assuming 256 bytes per chunk
        if (!app_chunk_tracker->chunk_received || !app_chunk_tracker->file_data) {
            LOG_ERR("Failed to allocate memory for chunk data");
            free(app_chunk_tracker->chunk_received);
            free(app_chunk_tracker);
            app_chunk_tracker = NULL;
            return;
        }
    }

    if (app_chunk_tracker->total_chunks != resp.total_chunks) {
        LOG_ERR("Inconsistent total chunks value: %d != %d", app_chunk_tracker->total_chunks, resp.total_chunks);
        return;
    }

    uint8_t binary_data[256];
    size_t binary_data_len = sizeof(binary_data);

    ret = base64_decode(binary_data, sizeof(binary_data), &binary_data_len, (const uint8_t *)resp.data, strlen(resp.data));
    if (ret < 0) {
        LOG_ERR("Failed to decode Base64 data for chunk %d", resp.chunk_idx);
        return;
    }

    memcpy(&app_chunk_tracker->file_data[resp.chunk_idx * binary_data_len], binary_data, binary_data_len);

    if (!app_chunk_tracker->chunk_received[resp.chunk_idx]) {
        app_chunk_tracker->chunk_received[resp.chunk_idx] = true;
        app_chunk_tracker->received_chunks++;
    }

    LOG_INF("Chunk %d/%d received for app: %s", app_chunk_tracker->received_chunks, app_chunk_tracker->total_chunks, resp.app_name);

    if (app_chunk_tracker->received_chunks == app_chunk_tracker->total_chunks) {
        LOG_INF("All chunks received for app: %s. Binary is ready in memory.", resp.app_name);

        // Process the complete binary in `app_chunk_tracker->file_data`

        free(app_chunk_tracker->chunk_received);
        free(app_chunk_tracker->file_data);
        free(app_chunk_tracker);
        app_chunk_tracker = NULL;

        LOG_INF("WASM binary assembled successfully for app: %s", resp.app_name);
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
