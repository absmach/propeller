#pragma once
#include <stdint.h>

/* Snapshot of system resource usage */
typedef struct {
    uint32_t heap_free;         /* bytes – current free internal heap */
    uint32_t heap_min;          /* bytes – all-time minimum free heap  */
    uint32_t heap_free_psram;   /* bytes – free PSRAM (0 if no PSRAM)  */
    uint32_t cpu_percent;       /* 0-100, averaged across both cores   */
    uint32_t uptime_ms;         /* ms since boot                       */
} metrics_t;

/**
 * Initialise the CPU-utilisation sampler.
 * Must be called once before metrics_sample().
 */
void metrics_init(void);

/**
 * Take a snapshot of current resource usage.
 * Thread-safe; can be called from any task.
 */
void metrics_sample(metrics_t *out);

/**
 * Print a one-line summary to the serial console.
 *   instances=N  heap=XKB  min=YKB  cpu=Z%  uptime=Ts
 */
void metrics_print(int instances, const metrics_t *m);
