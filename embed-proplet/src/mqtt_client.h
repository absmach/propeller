#ifndef MQTT_CLIENT_H
#define MQTT_CLIENT_H

#include <zephyr/net/mqtt.h>
#include <stdbool.h>

/* Extern variables */
extern bool mqtt_connected; // Indicates MQTT connection status

/* Function Prototypes */

/**
 * @brief Initialize the MQTT client and establish a connection to the broker.
 *
 * @return 0 on success, or a negative error code on failure.
 */
int mqtt_client_init_and_connect(void);

/**
 * @brief Publish a discovery announcement message to a specific topic.
 *
 * @param proplet_id The unique ID of the proplet.
 * @param channel_id The ID of the channel for the announcement.
 * @return 0 on success, or a negative error code on failure.
 */
int mqtt_client_discovery_announce(const char *proplet_id, const char *channel_id);

/**
 * @brief Subscribe to topics for a specific channel.
 *
 * @param channel_id The ID of the channel to subscribe to.
 * @return 0 on success, or a negative error code on failure.
 */
int mqtt_client_subscribe(const char *channel_id);

/**
 * @brief Process incoming MQTT messages and maintain the connection.
 */
void mqtt_client_process(void);

#endif /* MQTT_CLIENT_H */
