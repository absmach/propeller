#ifndef CLIENT_H
#define CLIENT_H

#include <zephyr/kernel.h>
#include <zephyr/net/mqtt.h>
#include <zephyr/net/socket.h>
#include <zephyr/logging/log.h>
#include <string.h>
#include <stdlib.h>
#include <stdio.h>

#include "wasm_export.h"

#ifdef __cplusplus
extern "C" {
#endif

int prepare_broker(void);

void init_mqtt_client(void);

int mqtt_connect_to_broker(void);

void mqtt_process_events(void);

#ifdef __cplusplus
}
#endif

#endif