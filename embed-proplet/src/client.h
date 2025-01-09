#ifndef CLIENT_H
#define CLIENT_H

#include <zephyr/net/mqtt.h>

void init_mqtt_client(const char *channel_id, const char *thing_id);

void mqtt_event_handler(struct mqtt_client *const client, const struct mqtt_evt *evt);

#endif 
