#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>
#include "wifi_manager.h"
#include "mqtt_client.h"

LOG_MODULE_REGISTER(main);

#define WIFI_SSID "Octavifi"
#define WIFI_PSK  "Unic0rn_2030"
#define PROPLET_ID "proplet1"
#define CHANNEL_ID "channel1"

const char *channel_id = CHANNEL_ID;

void main(void)
{
    LOG_INF("Starting Proplet...");

    wifi_manager_init();
    if (wifi_manager_connect(WIFI_SSID, WIFI_PSK) != 0) {
        LOG_ERR("Wi-Fi connection failed");
        return;
    }

    if (!wifi_manager_wait_for_connection(K_SECONDS(60))) {
        LOG_ERR("Wi-Fi connection timed out");
        return;
    }

    LOG_INF("Wi-Fi connected, proceeding with MQTT initialization");

    while (mqtt_client_connect() != 0) {
        LOG_ERR("MQTT client initialization failed. Retrying...");
        k_sleep(K_SECONDS(5));
    }

    LOG_INF("MQTT connected successfully.");

    if (publish_discovery(PROPLET_ID, CHANNEL_ID) != 0) {
        LOG_ERR("Discovery announcement failed");
        return;
    }

    if (subscribe(CHANNEL_ID) != 0) {
        LOG_ERR("Topic subscription failed");
        return;
    }

    LOG_INF("Proplet ready");

    while (1) {
        mqtt_client_process();

        if (mqtt_connected) {
            publish_alive_message(CHANNEL_ID);
        } else {
            LOG_WRN("MQTT client is not connected");
        }

        k_sleep(K_SECONDS(30));
    }
}
