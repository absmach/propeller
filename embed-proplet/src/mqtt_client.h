#ifndef MQTT_CLIENT_H
#define MQTT_CLIENT_H

#include <zephyr/net/mqtt.h>

/* Initialize and connect the MQTT client */
int mqtt_client_init_and_connect(void);

/* Process MQTT events */
void mqtt_client_process(void);

/* Publish discovery announcement */
int mqtt_client_discovery_announce(const char *proplet_id, const char *channel_id);

/* Subscribe to required topics */
int mqtt_client_subscribe(const char *channel_id);

#endif /* MQTT_CLIENT_H */
