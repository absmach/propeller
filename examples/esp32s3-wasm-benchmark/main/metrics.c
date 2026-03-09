#include "metrics.h"

#include <stdio.h>
#include <string.h>

#include "esp_heap_caps.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

/* ──────────────────────────────────────────────────────────────────────────
 * CPU utilisation via FreeRTOS run-time stats
 *
 * vTaskGetInfo(eRunning) triggers an IPC call on ESP-IDF SMP FreeRTOS
 * which can deadlock from task context. Instead, use the global
 * uxTaskGetSystemState() which is safe, and sum idle counters.
 * ─────────────────────────────────────────────────────────────────────────*/

#define MAX_TASKS 48  /* more than enough for our benchmark */

static uint64_t s_prev_total = 0;
static uint32_t s_prev_idle0 = 0;
static uint32_t s_prev_idle1 = 0;

static uint32_t get_idle_runtime(int core)
{
    static TaskStatus_t buf[MAX_TASKS];
    uint32_t total_runtime;
    UBaseType_t count = uxTaskGetSystemState(buf, MAX_TASKS, &total_runtime);

    const char *name = (core == 0) ? "IDLE0" : "IDLE1";
    for (UBaseType_t i = 0; i < count; i++) {
        if (strncmp(buf[i].pcTaskName, name, 5) == 0) {
            return buf[i].ulRunTimeCounter;
        }
    }
    return 0;
}

void metrics_init(void)
{
    s_prev_total = (uint64_t)esp_timer_get_time();
    s_prev_idle0 = get_idle_runtime(0);
    s_prev_idle1 = get_idle_runtime(1);
}

void metrics_sample(metrics_t *out)
{
    out->heap_free       = heap_caps_get_free_size(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    out->heap_min        = heap_caps_get_minimum_free_size(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    out->heap_free_psram = heap_caps_get_free_size(MALLOC_CAP_SPIRAM);
    out->uptime_ms       = (uint32_t)(esp_timer_get_time() / 1000ULL);

    uint64_t now     = (uint64_t)esp_timer_get_time();
    uint64_t elapsed = now - s_prev_total;

    uint32_t idle0_now = get_idle_runtime(0);
    uint32_t idle1_now = get_idle_runtime(1);

    uint64_t idle_delta = ((uint64_t)(idle0_now - s_prev_idle0) +
                           (uint64_t)(idle1_now - s_prev_idle1));
    uint64_t total_possible = elapsed * 2;  /* 2 cores */

    if (total_possible > 0 && idle_delta <= total_possible) {
        uint64_t busy = total_possible - idle_delta;
        out->cpu_percent = (uint32_t)((busy * 100ULL) / total_possible);
    } else {
        out->cpu_percent = 0;
    }

    s_prev_total = now;
    s_prev_idle0 = idle0_now;
    s_prev_idle1 = idle1_now;
}

void metrics_print(int instances, const metrics_t *m)
{
    if (m->heap_free_psram > 0) {
        printf("instances=%-3d  heap=%4uKB  min=%4uKB  psram=%4uKB  cpu=%3u%%  up=%us\n",
               instances,
               (unsigned)(m->heap_free      / 1024),
               (unsigned)(m->heap_min       / 1024),
               (unsigned)(m->heap_free_psram / 1024),
               (unsigned)m->cpu_percent,
               (unsigned)(m->uptime_ms / 1000));
    } else {
        printf("instances=%-3d  heap=%4uKB  min=%4uKB  cpu=%3u%%  up=%us\n",
               instances,
               (unsigned)(m->heap_free / 1024),
               (unsigned)(m->heap_min  / 1024),
               (unsigned)m->cpu_percent,
               (unsigned)(m->uptime_ms / 1000));
    }
    fflush(stdout);
}
