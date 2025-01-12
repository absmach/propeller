#include <zephyr/kernel.h>
#include <net/mqtt.h>
#include <net/socket.h>
#include <net/net_config.h>
#include <json.h>
#include <zephyr/random/random.h>
#include "wasm_handler.h"
#include <zephyr/net/wifi.h>
#include <zephyr/net/socket.h>
#include <zephyr/net/net_ip.h>
#include <zephyr/net/net_mgmt.h>
#include <zephyr/net/wifi_mgmt.h>
#include <zephyr/net/dhcpv4_server.h>

#define MQTT_BROKER_HOSTNAME "broker.supermq.example"
#define MQTT_BROKER_PORT 1883
#define PROPLET_ID "proplet_01"
#define CHANNEL_ID "channel_01"
#define TOPIC_REQUEST "/channels/" CHANNEL_ID "/messages/registry/request"
#define TOPIC_RESPONSE "/channels/" CHANNEL_ID "/messages/registry/response"

static struct mqtt_client client;
static struct sockaddr_storage broker_addr;
static uint8_t rx_buffer[512];
static uint8_t tx_buffer[512];
static int configure_broker(void)
{
    struct sockaddr_in *broker = (struct sockaddr_in *)&broker_addr;

    broker->sin_family = AF_INET;
    broker->sin_port = htons(MQTT_BROKER_PORT);

    int ret = net_addr_pton(AF_INET, MQTT_BROKER_HOSTNAME, &broker->sin_addr);
    if (ret != 0) {
        printk("Failed to configure broker address.\n");
        return -EINVAL;
    }

    return 0;
}

static void request_wasm_file(const char *app_name)
{
    char request_payload[128];
    snprintf(request_payload, sizeof(request_payload), "{\"app_name\":\"%s\"}", app_name);

    struct mqtt_publish_param pub_param = {
        .message_id = sys_rand32_get(),
        .message = {
            .topic = {
                .topic = {
                    .utf8 = TOPIC_REQUEST,
                    .size = strlen(TOPIC_REQUEST)
                },
                .qos = MQTT_QOS_1_AT_LEAST_ONCE
            },
            .payload = {
                .data = request_payload,
                .len = strlen(request_payload)
            }
        }
    };

    int ret = mqtt_publish(&client, &pub_param);
    if (ret) {
        printk("Failed to request Wasm file: %d\n", ret);
    } else {
        printk("Requested Wasm file: %s\n", app_name);
    }
}

static void mqtt_event_handler(struct mqtt_client *c, const struct mqtt_evt *evt)
{
    switch (evt->type) {
    case MQTT_EVT_CONNACK:
        printk("MQTT connected to broker.\n");
        break;

    case MQTT_EVT_PUBLISH: {
        const struct mqtt_publish_param *p = &evt->param.publish;

        if (strncmp(p->message.topic.topic.utf8, TOPIC_RESPONSE, p->message.topic.topic.size) == 0) {
            handle_wasm_chunk(p->message.payload.data, p->message.payload.len);
        }
        break;
    }
    case MQTT_EVT_DISCONNECT:
        printk("MQTT disconnected.\n");
        break;

    case MQTT_EVT_PUBACK:
        printk("MQTT publish acknowledged.\n");
        break;

    default:
        printk("Unhandled MQTT event: %d\n", evt->type);
        break;
    }
}

static int mqtt_client_setup(void)
{
    mqtt_client_init(&client);

    client.broker = &broker_addr;
    client.evt_cb = mqtt_event_handler;
    client.client_id.utf8 = PROPLET_ID;
    client.client_id.size = strlen(PROPLET_ID);
    client.protocol_version = MQTT_VERSION_3_1_1;

    client.rx_buf = rx_buffer;
    client.rx_buf_size = sizeof(rx_buffer);
    client.tx_buf = tx_buffer;
    client.tx_buf_size = sizeof(tx_buffer);

    return mqtt_connect(&client);
}

static int wifi_connect(void)
{
    struct net_if *sta_iface = net_if_get_wifi_sta();  // Get the station interface
    if (!sta_iface) {
        printk("No STA interface found.\n");
        return -ENODEV;
    }

    printk("STA interface found: %p\n", sta_iface);

    net_if_up(sta_iface);  // Bring up the STA interface
    printk("STA interface brought up.\n");

    struct wifi_connect_req_params params = {
        .ssid = CONFIG_WIFI_CREDENTIALS_STATIC_SSID,
        .ssid_length = strlen(CONFIG_WIFI_CREDENTIALS_STATIC_SSID),
        .psk = CONFIG_WIFI_CREDENTIALS_STATIC_PASSWORD,
        .psk_length = strlen(CONFIG_WIFI_CREDENTIALS_STATIC_PASSWORD),
        .channel = 6,
        .band = WIFI_FREQ_BAND_2_4_GHZ,
        .security = WIFI_SECURITY_TYPE_PSK,
    };

    printk("Connecting to Wi-Fi...\n");
    int ret = net_mgmt(NET_REQUEST_WIFI_CONNECT, sta_iface, &params, sizeof(params));
    if (ret) {
        printk("Wi-Fi connection failed: %d\n", ret);
        return ret;
    }

    printk("Wi-Fi connected successfully.\n");
    return 0;
}


void main(void)
{
    printk("Starting Proplet MQTT client on ESP32-S3...\n");

    int ret = wifi_connect();
    if (ret) {
        printk("Failed to connect to Wi-Fi.\n");
        return;
    }
    printk("Wi-Fi connected.\n");

    ret = configure_broker();
    if (ret) {
        printk("Failed to configure MQTT broker.\n");
        return;
    }

    ret = mqtt_client_setup();
    if (ret) {
        printk("Failed to set up MQTT client: %d\n", ret);
        return;
    }

    printk("MQTT client setup complete.\n");

    request_wasm_file("example_app");

    while (1) {
        mqtt_input(&client);
        mqtt_live(&client);
        k_sleep(K_MSEC(100));
    }
}
