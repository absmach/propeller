#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>
#include "client.h"

LOG_MODULE_REGISTER(my_proplet_main, LOG_LEVEL_DBG);

void main(void)
{
    LOG_INF("Proplet starting on ESP32-S3 with WAMR + MQTT...");

    if (prepare_broker() != 0) {
        LOG_ERR("Failed to prepare broker");
        return;
    }

    init_mqtt_client();

    if (mqtt_connect_to_broker() != 0) {
        LOG_ERR("Failed to connect to the MQTT broker");
        return;
    }

    while (1) {
        mqtt_process_events();

        k_sleep(K_MSEC(50));
    }
}
