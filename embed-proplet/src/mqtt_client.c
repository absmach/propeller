#include "mqtt_client.h"
#include <zephyr/logging/log.h>
#include <zephyr/net/socket.h>
#include <zephyr/kernel.h>
#include <zephyr/random/random.h>

LOG_MODULE_REGISTER(mqtt_client);

#define RX_BUFFER_SIZE 256
#define TX_BUFFER_SIZE 256

#define MQTT_BROKER_HOSTNAME "192.168.88.179" /* Replace with your broker's IP */
#define MQTT_BROKER_PORT 1883

#define DISCOVERY_TOPIC_TEMPLATE "channels/%s/messages/control/proplet/create"
#define START_TOPIC_TEMPLATE "channels/%s/messages/control/manager/start"
#define STOP_TOPIC_TEMPLATE "channels/%s/messages/control/manager/stop"

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
        LOG_INF("Message received on topic");
        break;

    case MQTT_EVT_SUBACK:
        LOG_INF("Subscribed successfully");
        break;

    case MQTT_EVT_PUBACK:
        LOG_INF("Message published successfully");
        break;

    default:
        LOG_WRN("Unhandled MQTT event [%d]", evt->type);
        break;
    }
}

int mqtt_client_init_and_connect(void)
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

int mqtt_client_discovery_announce(const char *proplet_id, const char *channel_id)
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

int mqtt_client_subscribe(const char *channel_id)
{
    char start_topic[128];
    snprintf(start_topic, sizeof(start_topic), START_TOPIC_TEMPLATE, channel_id);

    char stop_topic[128];
    snprintf(stop_topic, sizeof(stop_topic), STOP_TOPIC_TEMPLATE, channel_id);

    struct mqtt_topic topics[] = {
        {
            .topic = {
                .utf8 = (uint8_t *)start_topic,
                .size = strlen(start_topic),
            },
            .qos = MQTT_QOS_1_AT_LEAST_ONCE,
        },
        {
            .topic = {
                .utf8 = (uint8_t *)stop_topic,
                .size = strlen(stop_topic),
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
    }

    return ret;
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
