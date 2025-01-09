#ifndef CLIENT_H
#define CLIENT_H

#include <zephyr/kernel.h>
#include <zephyr/net/mqtt.h>
#include <zephyr/net/socket.h>
#include <zephyr/logging/log.h>
#include <string.h>
#include <stdlib.h>
#include <stdio.h>

/* If you use WAMR in client.c, include WAMR headers here too */
#include "wasm_export.h"

#ifdef __cplusplus
extern "C" {
#endif

/* Prepare the MQTT broker (resolve address) */
int prepare_broker(void);

/* Initialize the MQTT client structure */
void init_mqtt_client(void);

/* Connect to the MQTT broker */
int mqtt_connect_to_broker(void);

/* Handle MQTT input & keep-alive in a loop, typically called from main. */
void mqtt_process_events(void);

#ifdef __cplusplus
}
#endif

#endif /* CLIENT_H */
