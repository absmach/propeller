#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>
#include "wifi_manager.h"
// #include "mqtt_client.h"

LOG_MODULE_REGISTER(main);

#define WIFI_SSID "YourSSID"
#define WIFI_PSK  "YourPassword"
// #define PROPLET_ID "proplet-esp32s3"
// #define CHANNEL_ID "default_channel"

void main(void)
{
    LOG_INF("Proplet starting...");

    /* Initialize Wi-Fi */
    wifi_manager_init();

    /* Connect to Wi-Fi */
    int ret = wifi_manager_connect(WIFI_SSID, WIFI_PSK);
    if (ret != 0) {
        LOG_ERR("Failed to connect to Wi-Fi, ret=%d", ret);
        return;
    }

    // /* Initialize and connect MQTT client */
    // ret = mqtt_client_init_and_connect();
    // if (ret != 0) {
    //     LOG_ERR("Failed to initialize MQTT client, exiting");
    //     return;
    // }

    // /* Announce discovery */
    // ret = mqtt_client_discovery_announce(PROPLET_ID, CHANNEL_ID);
    // if (ret != 0) {
    //     LOG_ERR("Failed to publish discovery announcement, exiting");
    //     return;
    // }

    // /* Subscribe to topics */
    // ret = mqtt_client_subscribe(CHANNEL_ID);
    // if (ret != 0) {
    //     LOG_ERR("Failed to subscribe to topics, exiting");
    //     return;
    // }

    // /* Main loop for processing MQTT events */
    // while (1) {
    //     mqtt_client_process(); /* Process MQTT events */
    //     k_sleep(K_SECONDS(5)); /* Sleep for a while */
    // }
}
