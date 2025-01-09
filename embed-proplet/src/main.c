/*
 * main.c
 */

#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>
#include "client.h"

/* We'll use a separate log module for main if desired. */
LOG_MODULE_REGISTER(my_proplet_main, LOG_LEVEL_DBG);

void main(void)
{
    LOG_INF("Proplet starting on ESP32-S3 with WAMR + MQTT...");

    /* 1) Prepare the broker (resolve host/ip) */
    if (prepare_broker() != 0) {
        LOG_ERR("Failed to prepare broker");
        return;
    }

    /* 2) Initialize MQTT client struct */
    init_mqtt_client();

    /* 3) Connect to broker */
    if (mqtt_connect_to_broker() != 0) {
        LOG_ERR("Failed to connect to the MQTT broker");
        return;
    }

    /* 4) Application main loop */
    while (1) {
        /* Let the client handle MQTT input and keep-alive */
        mqtt_process_events();

        /* Sleep some ms to avoid busy loops */
        k_sleep(K_MSEC(50));
    }
}
