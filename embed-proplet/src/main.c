#include <zephyr/kernel.h>
#include <zephyr/net/mqtt.h>
#include <zephyr/logging/log.h>
#include "client.h"

LOG_MODULE_REGISTER(main, LOG_LEVEL_DBG);

#define CHANNEL_ID "my_channel_id"
#define THING_ID   "my_thing_id"

static struct mqtt_client mqtt_client;
static struct sockaddr_storage broker_storage;

void main(void)
{
    LOG_INF("Starting Proplet MQTT Client");

    init_mqtt_client(CHANNEL_ID, THING_ID);

    struct sockaddr_in *broker = (struct sockaddr_in *)&broker_storage;
    broker->sin_family = AF_INET;
    broker->sin_port = htons(MQTT_BROKER_PORT);
    if (net_addr_pton(AF_INET, MQTT_BROKER_HOSTNAME, &broker->sin_addr) < 0) {
        LOG_ERR("Failed to configure broker address");
        return;
    }

    mqtt_client.broker = &broker_storage;
    mqtt_client.evt_cb = mqtt_event_handler;

    if (mqtt_connect(&mqtt_client) != 0) {
        LOG_ERR("Failed to connect to MQTT broker");
        return;
    }

    LOG_INF("Connected to MQTT broker. Entering event loop.");

    while (true) {
        if (mqtt_input(&mqtt_client) < 0) {
            LOG_ERR("MQTT input processing failed");
            break;
        }
        if (mqtt_live(&mqtt_client) < 0) {
            LOG_ERR("MQTT keepalive failed");
            break;
        }

        k_sleep(K_MSEC(100));
    }

    mqtt_disconnect(&mqtt_client);
    LOG_INF("Disconnected from MQTT broker.");
}
