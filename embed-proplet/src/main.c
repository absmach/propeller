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

    wifi_manager_connect(WIFI_SSID, WIFI_PSK);

    mqtt_client_connect(PROPLET_ID, CHANNEL_ID);

    publish_discovery(PROPLET_ID, CHANNEL_ID);

    subscribe(CHANNEL_ID);

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
