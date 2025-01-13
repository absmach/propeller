#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>
#include "wifi_manager.h"
#include "mqtt_client.h"

LOG_MODULE_REGISTER(main);

#define WIFI_SSID "Octavifi"
#define WIFI_PSK  "Unic0rn_2030"
#define PROPLET_ID "proplet-esp32s3"
#define CHANNEL_ID "default_channel"

void main(void)
{
    LOG_INF("Starting Proplet...");

    /* Initialize Wi-Fi */
    wifi_manager_init();
    if (wifi_manager_connect(WIFI_SSID, WIFI_PSK) != 0) {
        LOG_ERR("Wi-Fi connection failed");
        return;
    }

    /* Wait for Wi-Fi connection */
    if (!wifi_manager_wait_for_connection(K_SECONDS(10))) {
        LOG_ERR("Wi-Fi connection timed out");
        return;
    }
    LOG_INF("Wi-Fi connected, proceeding with MQTT initialization");

    /* Initialize MQTT client */
    if (mqtt_client_init_and_connect() != 0) {
        LOG_ERR("MQTT client initialization failed");
        return;
    }

    /* Publish discovery announcement */
    if (mqtt_client_discovery_announce(PROPLET_ID, CHANNEL_ID) != 0) {
        LOG_ERR("Discovery announcement failed");
        return;
    }

    /* Subscribe to topics */
    if (mqtt_client_subscribe(CHANNEL_ID) != 0) {
        LOG_ERR("Topic subscription failed");
        return;
    }

    LOG_INF("Proplet ready");

    /* Main loop for MQTT processing */
    while (1) {
        mqtt_client_process();
        k_sleep(K_SECONDS(5));
    }
}
