#ifndef TASK_MONITOR_H
#define TASK_MONITOR_H

#include <stdbool.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#define MAX_MONITORED_TASKS 4
#define MAX_TASK_ID_LEN 64

/**
 * @brief Process-level metrics for a single sample.
 */
typedef struct {
    double cpu_percent;
    uint32_t memory_bytes;
    double memory_percent;
    int64_t uptime_seconds;
    uint32_t thread_count;
} process_metrics_t;

/**
 * @brief Aggregated metrics over multiple samples.
 */
typedef struct {
    double avg_cpu_usage;
    double max_cpu_usage;
    uint64_t avg_memory_usage;
    uint64_t max_memory_usage;
    int sample_count;
} aggregated_metrics_t;

/**
 * @brief Combined task metrics with current and aggregated data.
 */
typedef struct {
    char task_id[MAX_TASK_ID_LEN];
    bool active;
    int64_t start_time_ms;
    process_metrics_t current;
    aggregated_metrics_t aggregated;
} task_metrics_t;

/**
 * @brief Initialize the task monitor subsystem.
 *
 * Must be called before any other task_monitor functions.
 *
 * @return 0 on success, negative errno on failure.
 */
int task_monitor_init(void);

/**
 * @brief Start monitoring a task.
 *
 * Call this before WASM module execution begins.
 *
 * @param task_id Unique identifier for the task.
 * @return 0 on success, negative errno on failure.
 */
int task_monitor_start(const char *task_id);

/**
 * @brief Record a metrics sample for a task.
 *
 * Call this periodically or after task execution to capture metrics.
 *
 * @param task_id Unique identifier for the task.
 * @return 0 on success, negative errno on failure.
 */
int task_monitor_sample(const char *task_id);

/**
 * @brief Stop monitoring a task.
 *
 * Call this after WASM execution completes or is stopped.
 *
 * @param task_id Unique identifier for the task.
 * @return 0 on success, negative errno on failure.
 */
int task_monitor_stop(const char *task_id);

/**
 * @brief Get metrics for a specific task.
 *
 * @param task_id Unique identifier for the task.
 * @param metrics Pointer to task_metrics_t to populate.
 * @return 0 on success, negative errno on failure.
 */
int task_monitor_get_metrics(const char *task_id, task_metrics_t *metrics);

/**
 * @brief Check if a task is currently being monitored.
 *
 * @param task_id Unique identifier for the task.
 * @return true if task is active, false otherwise.
 */
bool task_monitor_is_active(const char *task_id);

/**
 * @brief Get the ID of the first active monitored task.
 *
 * Useful for single-task scenarios common in embedded systems.
 *
 * @param task_id_out Buffer to store the task ID (must be at least MAX_TASK_ID_LEN).
 * @return 0 on success, -ENOENT if no active task.
 */
int task_monitor_get_active_task_id(char *task_id_out);

/**
 * @brief Get count of active monitored tasks.
 *
 * @return Number of currently monitored tasks.
 */
int task_monitor_get_active_count(void);

/**
 * @brief Get task ID for an active monitored task at index.
 *
 * Use with task_monitor_get_active_count() to iterate through all active tasks.
 *
 * @param index Index of the active task (0 to count-1).
 * @param task_id_out Buffer to store the task ID (must be at least MAX_TASK_ID_LEN).
 * @return 0 on success, -EINVAL if index is out of bounds or task_id_out is NULL.
 */
int task_monitor_get_active_task_id_at(int index, char *task_id_out);

#ifdef __cplusplus
}
#endif

#endif /* TASK_MONITOR_H */
