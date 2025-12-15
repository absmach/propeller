#ifndef MQTT_CLIENT_H
#define MQTT_CLIENT_H

#include <stdbool.h>
#include <stddef.h>
#include <zephyr/net/mqtt.h>

#define PAYLOAD_BUFFER_SIZE 256

extern bool mqtt_connected;

/**
 * @brief Initialize the MQTT client and establish a connection to the broker.
 *
 * Note: This connects using the provided proplet_id as the MQTT username.
 * Password is not used in the current 3-argument API.
 *
 * @param domain_id   Domain ID used for topic generation (e.g., m/<domain>/c/<channel>/...).
 * @param proplet_id  Proplet identity used for manager tracking and alive/metrics payloads.
 * @param channel_id  Channel ID used for topic generation.
 *
 * @return 0 on success, negative errno on failure.
 */
int mqtt_client_connect(const char *domain_id,
                        const char *proplet_id,
                        const char *channel_id);

/**
 * @brief Subscribe to the manager/control topics for a channel.
 *
 * @param domain_id  Domain ID used for topic generation.
 * @param channel_id Channel ID used for topic generation.
 *
 * @return 0 on success, negative errno on failure.
 */
int subscribe(const char *domain_id, const char *channel_id);

/**
 * @brief Publish a message to a specific MQTT topic.
 *
 * @param domain_id       Domain ID used for topic generation.
 * @param channel_id      Channel ID used for topic generation.
 * @param topic_template  Topic template string that accepts (domain_id, channel_id).
 * @param payload         Null-terminated JSON payload string.
 *
 * @return 0 on success, negative errno on failure.
 */
int publish(const char *domain_id,
            const char *channel_id,
            const char *topic_template,
            const char *payload);

/**
 * @brief Publish a periodic "alive" message to notify the manager of liveliness.
 *
 * Uses the proplet_id captured during mqtt_client_connect().
 *
 * @param domain_id  Domain ID used for topic generation.
 * @param channel_id Channel ID used for topic generation.
 */
void publish_alive_message(const char *domain_id, const char *channel_id);

/**
 * @brief Publish periodic CPU/memory metrics.
 *
 * Payload shape: { proplet_id, namespace, metrics: {...} }
 *
 * @param domain_id   Domain ID used for topic generation.
 * @param channel_id  Channel ID used for topic generation.
 * @param proplet_id  Proplet identity.
 * @param namespace   Namespace label (e.g. "embedded").
 */
void publish_metrics_message(const char *domain_id,
                             const char *channel_id,
                             const char *proplet_id,
                             const char *namespace);

/**
 * @brief Publish a request to fetch a file from the registry.
 *
 * @param domain_id  Domain ID used for topic generation.
 * @param channel_id Channel ID used for topic generation.
 * @param app_name   Registry app name / image reference to fetch.
 */
void publish_registry_request(const char *domain_id,
                              const char *channel_id,
                              const char *app_name);

/**
 * @brief Publish a discovery message when the Proplet comes online.
 *
 * @param domain_id   Domain ID used for topic generation.
 * @param proplet_id  Proplet identity.
 * @param channel_id  Channel ID used for topic generation.
 *
 * @return 0 on success, negative errno on failure.
 */
int publish_discovery(const char *domain_id,
                      const char *proplet_id,
                      const char *channel_id);

/**
 * @brief Publish the results of a completed task.
 *
 * @param domain_id  Domain ID used for topic generation.
 * @param channel_id Channel ID used for topic generation.
 * @param task_id    Task identifier.
 * @param results    Result string (will be JSON-escaped by caller if needed).
 */
void publish_results(const char *domain_id,
                     const char *channel_id,
                     const char *task_id,
                     const char *results);

/**
 * @brief Process incoming MQTT messages and maintain keepalive.
 */
void mqtt_client_process(void);

/**
 * @brief Handle the start command received via MQTT.
 *
 * @param payload JSON payload for the start command.
 */
void handle_start_command(const char *payload);

/**
 * @brief Handle the stop command received via MQTT.
 *
 * @param payload JSON payload for the stop command.
 */
void handle_stop_command(const char *payload);

/**
 * @brief Handle registry response that contains the base64-encoded WASM.
 *
 * @param payload JSON payload containing app_name + data (base64).
 *
 * @return 0 on success, negative errno on failure.
 */
int handle_registry_response(const char *payload);

#endif /* MQTT_CLIENT_H */
