#include "metrics.h"

#include <stdio.h>
#include <string.h>

#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>
#include <zephyr/sys/mem_stats.h>

LOG_MODULE_REGISTER(metrics);

int malloc_runtime_stats_get(struct sys_memory_stats *stats);

static uint32_t heap_free_bytes(void)
{
    struct sys_memory_stats stats;

    if (malloc_runtime_stats_get(&stats) == 0) {
        return (uint32_t)stats.free_bytes;
    }
    return 0;
}

static uint32_t heap_min_free_bytes(void)
{
    struct sys_memory_stats stats;

    if (malloc_runtime_stats_get(&stats) == 0) {
        size_t total = stats.free_bytes + stats.allocated_bytes;

        if (stats.max_allocated_bytes <= total) {
            return (uint32_t)(total - stats.max_allocated_bytes);
        }
    }
    return 0;
}

static uint64_t s_prev_exec_cycles;
static uint64_t s_prev_busy_cycles;

void metrics_init(void)
{
    k_thread_runtime_stats_t stats;

    k_thread_runtime_stats_all_get(&stats);
    s_prev_exec_cycles = stats.execution_cycles;
    s_prev_busy_cycles = stats.total_cycles;
}

void metrics_sample(metrics_t *out)
{
    out->heap_free = heap_free_bytes();
    out->heap_min  = heap_min_free_bytes();
    out->uptime_ms = (uint32_t)k_uptime_get();

    k_thread_runtime_stats_t stats;

    k_thread_runtime_stats_all_get(&stats);

    uint64_t exec_delta = stats.execution_cycles - s_prev_exec_cycles;
    uint64_t busy_delta = stats.total_cycles     - s_prev_busy_cycles;

    if (exec_delta > 0 && busy_delta <= exec_delta) {
        out->cpu_percent = (uint32_t)((busy_delta * 100ULL) / exec_delta);
    } else {
        out->cpu_percent = 0;
    }

    s_prev_exec_cycles = stats.execution_cycles;
    s_prev_busy_cycles = stats.total_cycles;
}

void metrics_print(int instances, const metrics_t *m)
{
    printf("instances=%-3d  heap=%4uKB  min=%4uKB  cpu=%3u%%  up=%us\n",
           instances,
           (unsigned)(m->heap_free / 1024),
           (unsigned)(m->heap_min  / 1024),
           (unsigned)m->cpu_percent,
           (unsigned)(m->uptime_ms / 1000));
}
