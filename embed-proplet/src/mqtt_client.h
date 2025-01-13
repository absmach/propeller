#ifndef MQTT_CLIENT_H
#define MQTT_CLIENT_H

#include <zephyr/net/mqtt.h>
#include <stdbool.h>

extern bool mqtt_connected;

int mqtt_client_init_and_connect(void);
int mqtt_client_discovery_announce(const char *proplet_id, const char *channel_id);
int mqtt_client_subscribe(const char *channel_id);
void mqtt_client_process(void);

#endif /* MQTT_CLIENT_H */
