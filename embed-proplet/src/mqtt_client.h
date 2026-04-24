#ifndef MQTT_CLIENT_H
#define MQTT_CLIENT_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <zephyr/net/mqtt.h>

#define PAYLOAD_BUFFER_SIZE 1024

#define MAX_ID_LEN         64
#define MAX_NAME_LEN       64
#define MAX_STATE_LEN      16
#define MAX_URL_LEN        256
#define MAX_TIMESTAMP_LEN  32
#define MAX_BASE64_LEN     1024
#define MAX_INPUTS         16
#define MAX_RESULTS        16

struct task {
  char id[MAX_ID_LEN];
  char name[MAX_NAME_LEN];
  char state[MAX_STATE_LEN];
  char image_url[MAX_URL_LEN];
  char mode[MAX_NAME_LEN];

  char file[MAX_BASE64_LEN];
  size_t file_len;

  uint64_t inputs[MAX_INPUTS];
  size_t inputs_count;
  uint64_t results[MAX_RESULTS];
  size_t results_count;

  char start_time[MAX_TIMESTAMP_LEN];
  char finish_time[MAX_TIMESTAMP_LEN];
  char created_at[MAX_TIMESTAMP_LEN];
  char updated_at[MAX_TIMESTAMP_LEN];

  bool is_fl_task;

  char fl_round_id_str[32];
  char fl_format[MAX_NAME_LEN];
  char fl_num_samples_str[32];

  char round_id[MAX_ID_LEN];
  char model_uri[MAX_URL_LEN];
  char hyperparams[512];
  bool is_fml_task;

  char proplet_id[MAX_ID_LEN];
  char model_data[4096];
  char dataset_data[4096];
  char coordinator_url[MAX_URL_LEN];
  char model_registry_url[MAX_URL_LEN];
  char data_store_url[MAX_URL_LEN];
  bool model_data_fetched;
  bool dataset_data_fetched;
};

extern struct task g_current_task;

extern bool mqtt_connected;

/**
 * @brief Initialize the MQTT client and establish a connection to the broker.
 *
 * Connects using proplet_id as MQTT client ID and username, client_key as password.
 *
 * @param domain_id   Domain ID used for topic generation (e.g., m/<domain>/c/<channel>/...).
 * @param proplet_id  Proplet identity (SuperMQ client_id) — used as MQTT username.
 * @param client_key  SuperMQ client secret — used as MQTT password.
 * @param channel_id  Channel ID used for topic generation.
 *
 * @return 0 on success, negative errno on failure.
 */
int mqtt_client_connect(const char *domain_id,
                        const char *proplet_id,
                        const char *client_key,
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
 * Auto-detected fields (ip, environment, os, hostname, cpu_arch,
 * total_memory_bytes, wasm_runtime) are resolved inside the function.
 *
 * @param domain_id   Domain ID used for topic generation.
 * @param proplet_id  Proplet identity.
 * @param channel_id  Channel ID used for topic generation.
 * @param description Human-readable description of this proplet (may be empty).
 * @param tags        Comma-separated tag list, e.g. "gateway,prod" (may be empty).
 * @param location    Physical or logical location string (may be empty).
 * @param version     Firmware/software version string.
 *
 * @return 0 on success, negative errno on failure.
 */
int publish_discovery(const char *domain_id,
                      const char *proplet_id,
                      const char *channel_id,
                      const char *description,
                      const char *tags,
                      const char *location,
                      const char *version);

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
 * @brief Publish the results of a completed task with optional error message.
 *
 * @param domain_id  Domain ID used for topic generation.
 * @param channel_id Channel ID used for topic generation.
 * @param task_id    Task identifier.
 * @param results    Result string (will be JSON-escaped by caller if needed).
 * @param error_msg  Optional error message (NULL if no error).
 */
void publish_results_with_error(const char *domain_id,
                                const char *channel_id,
                                const char *task_id,
                                const char *results,
                                const char *error_msg);

/** @brief Publish task-level metrics for a specific task.
 *
 * Publishes CPU, memory, and timing metrics for a running or recently
 * completed WASM task.
 *
 * @param domain_id   Domain ID used for topic generation.
 * @param channel_id  Channel ID used for topic generation.
 * @param task_id     Task identifier being monitored.
 * @param proplet_id  Proplet identity.
 */
void publish_task_metrics(const char *domain_id,
                          const char *channel_id,
                          const char *task_id,
                          const char *proplet_id);

/**
 * @brief Publish metrics for all active monitored tasks.
 *
 * Iterates through all currently monitored tasks and publishes their metrics.
 *
 * @param domain_id   Domain ID used for topic generation.
 * @param channel_id  Channel ID used for topic generation.
 * @param proplet_id  Proplet identity.
 */
void publish_active_task_metrics(const char *domain_id,
                                 const char *channel_id,
                                 const char *proplet_id);

/**
 * @brief Publish WASM execution metrics.
 *
 * Publishes aggregate WASM execution statistics (execution count, timing, errors).
 *
 * @param domain_id   Domain ID used for topic generation.
 * @param channel_id  Channel ID used for topic generation.
 * @param proplet_id  Proplet identity.
 */
void publish_wasm_metrics(const char *domain_id,
                          const char *channel_id,
                          const char *proplet_id);

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
