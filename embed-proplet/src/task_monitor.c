#include "task_monitor.h"

#include <errno.h>
#include <string.h>

#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>

#if defined(CONFIG_CPU_LOAD)
#include <zephyr/debug/cpu_load.h>
#endif

#if defined(CONFIG_SYS_HEAP_RUNTIME_STATS)
#include <zephyr/sys/sys_heap.h>
extern struct sys_heap _system_heap;
#endif

LOG_MODULE_REGISTER(task_monitor);

typedef struct {
  bool in_use;
  char task_id[MAX_TASK_ID_LEN];
  int64_t start_time_ms;
  process_metrics_t current;
  double cpu_sum;
  double cpu_max;
  uint64_t memory_sum;
  uint64_t memory_max;
  int sample_count;
} monitored_task_t;

static monitored_task_t g_monitored_tasks[MAX_MONITORED_TASKS];
static bool g_initialized = false;
static K_MUTEX_DEFINE(g_task_monitor_mutex);

static int find_task_slot(const char *task_id);
static int find_free_slot(void);
static void collect_current_metrics(process_metrics_t *metrics,
                                    int64_t start_time_ms);

#if defined(CONFIG_THREAD_MONITOR)
static void thread_count_cb(const struct k_thread *thread, void *user_data)
{
  ARG_UNUSED(thread);
  int *count = (int *)user_data;
  (*count)++;
}

static int get_thread_count(void)
{
  int count = 0;
  k_thread_foreach_unlocked(thread_count_cb, &count);
  return count;
}
#endif

int task_monitor_init(void)
{
  if (g_initialized) {
    return 0;
  }

  memset(g_monitored_tasks, 0, sizeof(g_monitored_tasks));
  g_initialized = true;

  LOG_INF("Task monitor initialized (max tasks: %d)", MAX_MONITORED_TASKS);
  return 0;
}

int task_monitor_start(const char *task_id)
{
  if (!g_initialized) {
    LOG_ERR("Task monitor not initialized");
    return -EINVAL;
  }

  if (task_id == NULL || strlen(task_id) == 0) {
    LOG_ERR("Invalid task ID");
    return -EINVAL;
  }

  k_mutex_lock(&g_task_monitor_mutex, K_FOREVER);

  int existing = find_task_slot(task_id);
  int slot;
  if (existing >= 0) {
    LOG_WRN("Task %s already being monitored, clearing and restarting",
            task_id);
    slot = existing;
  } else {
    slot = find_free_slot();
    if (slot < 0) {
      LOG_ERR("No free monitoring slots (max %d)", MAX_MONITORED_TASKS);
      k_mutex_unlock(&g_task_monitor_mutex);
      return -ENOMEM;
    }
  }

  monitored_task_t *task = &g_monitored_tasks[slot];
  memset(task, 0, sizeof(*task));

  strncpy(task->task_id, task_id, MAX_TASK_ID_LEN - 1);
  task->task_id[MAX_TASK_ID_LEN - 1] = '\0';
  task->start_time_ms = k_uptime_get();

  collect_current_metrics(&task->current, task->start_time_ms);

  task->cpu_sum = task->current.cpu_percent;
  task->cpu_max = task->current.cpu_percent;
  task->memory_sum = task->current.memory_bytes;
  task->memory_max = task->current.memory_bytes;
  task->sample_count = 1;

  task->in_use = true;

  k_mutex_unlock(&g_task_monitor_mutex);

  LOG_INF("Started monitoring task: %s", task_id);
  return 0;
}

int task_monitor_sample(const char *task_id)
{
  if (!g_initialized) {
    return -EINVAL;
  }

  k_mutex_lock(&g_task_monitor_mutex, K_FOREVER);

  int slot = find_task_slot(task_id);
  if (slot < 0) {
    LOG_WRN("Task %s not found for sampling", task_id);
    k_mutex_unlock(&g_task_monitor_mutex);
    return -ENOENT;
  }

  monitored_task_t *task = &g_monitored_tasks[slot];

  collect_current_metrics(&task->current, task->start_time_ms);

  task->cpu_sum += task->current.cpu_percent;
  if (task->current.cpu_percent > task->cpu_max) {
    task->cpu_max = task->current.cpu_percent;
  }

  task->memory_sum += task->current.memory_bytes;
  if (task->current.memory_bytes > task->memory_max) {
    task->memory_max = task->current.memory_bytes;
  }

  task->sample_count++;

  k_mutex_unlock(&g_task_monitor_mutex);

  LOG_DBG("Sampled task %s: cpu=%.1f%%, mem=%u bytes, samples=%d", task_id,
          task->current.cpu_percent, task->current.memory_bytes,
          task->sample_count);

  return 0;
}

int task_monitor_stop(const char *task_id)
{
  if (!g_initialized) {
    return -EINVAL;
  }

  k_mutex_lock(&g_task_monitor_mutex, K_FOREVER);

  int slot = find_task_slot(task_id);
  if (slot < 0) {
    LOG_WRN("Task %s not found to stop", task_id);
    k_mutex_unlock(&g_task_monitor_mutex);
    return -ENOENT;
  }

  monitored_task_t *task = &g_monitored_tasks[slot];

  collect_current_metrics(&task->current, task->start_time_ms);
  task->cpu_sum += task->current.cpu_percent;
  task->memory_sum += task->current.memory_bytes;
  if (task->current.cpu_percent > task->cpu_max) {
    task->cpu_max = task->current.cpu_percent;
  }
  if (task->current.memory_bytes > task->memory_max) {
    task->memory_max = task->current.memory_bytes;
  }
  task->sample_count++;

  task->in_use = false;

  k_mutex_unlock(&g_task_monitor_mutex);

  LOG_INF("Stopped monitoring task: %s (samples: %d)", task_id,
          task->sample_count);
  return 0;
}

int task_monitor_get_metrics(const char *task_id, task_metrics_t *metrics)
{
  if (!g_initialized || metrics == NULL) {
    return -EINVAL;
  }

  k_mutex_lock(&g_task_monitor_mutex, K_FOREVER);

  int slot = find_task_slot(task_id);
  if (slot < 0) {
    k_mutex_unlock(&g_task_monitor_mutex);
    return -ENOENT;
  }

  monitored_task_t *task = &g_monitored_tasks[slot];

  memset(metrics, 0, sizeof(*metrics));
  strncpy(metrics->task_id, task->task_id, MAX_TASK_ID_LEN - 1);
  metrics->active = task->in_use;
  metrics->start_time_ms = task->start_time_ms;

  memcpy(&metrics->current, &task->current, sizeof(process_metrics_t));

  if (task->sample_count > 0) {
    metrics->aggregated.avg_cpu_usage = task->cpu_sum / task->sample_count;
    metrics->aggregated.max_cpu_usage = task->cpu_max;
    metrics->aggregated.avg_memory_usage = task->memory_sum / task->sample_count;
    metrics->aggregated.max_memory_usage = task->memory_max;
    metrics->aggregated.sample_count = task->sample_count;
  }

  k_mutex_unlock(&g_task_monitor_mutex);

  return 0;
}

bool task_monitor_is_active(const char *task_id)
{
  if (!g_initialized || task_id == NULL) {
    return false;
  }

  k_mutex_lock(&g_task_monitor_mutex, K_FOREVER);

  int slot = find_task_slot(task_id);

  k_mutex_unlock(&g_task_monitor_mutex);

  return (slot >= 0);
}

int task_monitor_get_active_task_id(char *task_id_out)
{
  if (!g_initialized || task_id_out == NULL) {
    return -EINVAL;
  }

  k_mutex_lock(&g_task_monitor_mutex, K_FOREVER);

  for (int i = 0; i < MAX_MONITORED_TASKS; i++) {
    if (g_monitored_tasks[i].in_use) {
      strncpy(task_id_out, g_monitored_tasks[i].task_id, MAX_TASK_ID_LEN - 1);
      task_id_out[MAX_TASK_ID_LEN - 1] = '\0';
      k_mutex_unlock(&g_task_monitor_mutex);
      return 0;
    }
  }

  k_mutex_unlock(&g_task_monitor_mutex);
  return -ENOENT;
}

int task_monitor_get_active_count(void)
{
  if (!g_initialized) {
    return 0;
  }

  k_mutex_lock(&g_task_monitor_mutex, K_FOREVER);

  int count = 0;
  for (int i = 0; i < MAX_MONITORED_TASKS; i++) {
    if (g_monitored_tasks[i].in_use) {
      count++;
    }
  }

  k_mutex_unlock(&g_task_monitor_mutex);

  return count;
}

int task_monitor_get_active_task_id_at(int index, char *task_id_out)
{
  if (!g_initialized || task_id_out == NULL || index < 0) {
    return -EINVAL;
  }

  k_mutex_lock(&g_task_monitor_mutex, K_FOREVER);

  int active_index = 0;
  for (int i = 0; i < MAX_MONITORED_TASKS; i++) {
    if (g_monitored_tasks[i].in_use) {
      if (active_index == index) {
        strncpy(task_id_out, g_monitored_tasks[i].task_id, MAX_TASK_ID_LEN - 1);
        task_id_out[MAX_TASK_ID_LEN - 1] = '\0';
        k_mutex_unlock(&g_task_monitor_mutex);
        return 0;
      }
      active_index++;
    }
  }

  k_mutex_unlock(&g_task_monitor_mutex);
  return -ENOENT;
}

static int find_task_slot(const char *task_id)
{
  for (int i = 0; i < MAX_MONITORED_TASKS; i++) {
    if (g_monitored_tasks[i].in_use &&
        strcmp(g_monitored_tasks[i].task_id, task_id) == 0) {
      return i;
    }
  }
  return -1;
}

static int find_free_slot(void)
{
  for (int i = 0; i < MAX_MONITORED_TASKS; i++) {
    if (!g_monitored_tasks[i].in_use) {
      return i;
    }
  }
  return -1;
}

static void collect_current_metrics(process_metrics_t *metrics,
                                    int64_t start_time_ms)
{
  memset(metrics, 0, sizeof(*metrics));

#if defined(CONFIG_CPU_LOAD)
  metrics->cpu_percent = (double)cpu_load_get(false) / 10.0;
#else
  metrics->cpu_percent = 0.0;
#endif

#if defined(CONFIG_SYS_HEAP_RUNTIME_STATS)
  struct sys_memory_stats stat;

  if (sys_heap_runtime_stats_get(&_system_heap, &stat) == 0) {
    metrics->memory_bytes = stat.allocated_bytes;
    uint32_t total = stat.allocated_bytes + stat.free_bytes;
    if (total > 0) {
      metrics->memory_percent = (double)stat.allocated_bytes * 100.0 / total;
    }
  }
#else
  metrics->memory_bytes = 0;
  metrics->memory_percent = 0.0;
#endif

  int64_t now_ms = k_uptime_get();
  metrics->uptime_seconds = (now_ms - start_time_ms) / 1000;

#if defined(CONFIG_THREAD_MONITOR)
  metrics->thread_count = get_thread_count();
#else
  metrics->thread_count = 1;
#endif
}
