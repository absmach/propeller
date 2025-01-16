#ifndef MQTT_CLIENT_H
#define MQTT_CLIENT_H

#include <zephyr/net/mqtt.h>
#include <stdbool.h>

#define PAYLOAD_BUFFER_SIZE 256

extern bool mqtt_connected;

/**
 * @brief Initialize the MQTT client and establish a connection to the broker.
 *
 * @return 0 on success, or a negative error code on failure.
 */
int mqtt_client_connect(const char *proplet_id, const char *channel_id);

/**
 * @brief Initialize and connect the MQTT client to the broker.
 *
 * @param proplet_id Unique ID of the proplet for the Last Will message.
 * @param channel_id Channel ID for the Last Will topic.
 * @return 0 on success, or a negative error code on failure.
 */
int mqtt_client_connect(const char *proplet_id, const char *channel_id);


/**
 * @brief Subscribe to topics for a specific channel.
 *
 * @param channel_id The ID of the channel to subscribe to.
 * @return 0 on success, or a negative error code on failure.
 */
int subscribe(const char *channel_id);

/**
 * @brief Publish a message to a specific MQTT topic.
 *
 * @param channel_id The ID of the channel for dynamic topic generation.
 * @param topic_template The template of the topic.
 * @param payload The payload to be published.
 * @return 0 on success, or a negative error code on failure.
 */
int publish(const char *channel_id, const char *topic_template, const char *payload);

/**
 * @brief Publish a periodic "alive" message to notify the manager of liveliness.
 *
 * @param channel_id The ID of the channel for dynamic topic generation.
 */
void publish_alive_message(const char *channel_id);

/**
 * @brief Publish a request to fetch a file from the registry.
 *
 * @param channel_id The ID of the channel for dynamic topic generation.
 * @param app_name The name of the application to fetch.
 */
void publish_registry_request(const char *channel_id, const char *app_name);

/**
 * @brief Publish a discovery message when the Proplet comes online for the first time.
 *
 * @param proplet_id The unique ID of the proplet.
 * @param channel_id The ID of the channel for the announcement.
 * @return 0 on success, or a negative error code on failure.
 */
int publish_discovery(const char *proplet_id, const char *channel_id);

/**
 * @brief Publish the results of a completed task.
 *
 * @param channel_id The ID of the channel for dynamic topic generation.
 * @param task_id The ID of the task whose results are being published.
 * @param results The results of the task.
 */
void publish_results(const char *channel_id, const char *task_id, const char *results);

/**
 * @brief Process incoming MQTT messages and maintain the connection.
 */
void mqtt_client_process(void);

/**
 * @brief Handle the start command received via MQTT.
 *
 * @param payload The JSON payload for the start command.
 */
void handle_start_command(const char *payload);

/**
 * @brief Handle the stop command received via MQTT.
 *
 * @param payload The JSON payload for the stop command.
 */
void handle_stop_command(const char *payload);

/**
 * @brief Handle registry responses for WASM binary chunks.
 *
 * This function processes incoming chunks of WASM binaries sent by the registry proxy.
 * It logs details of each chunk, tracks progress, and assembles the chunks into a complete
 * binary file when all chunks are received.
 *
 * @param payload The JSON payload containing the chunk details.
 */
int handle_registry_response(const char *payload);

#endif /* MQTT_CLIENT_H */
