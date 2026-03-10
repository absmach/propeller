#pragma once

#include <stdint.h>

typedef struct {
    uint32_t heap_free;       /* bytes free in the system heap           */
    uint32_t heap_min;        /* minimum ever free (high-water of usage) */
    uint32_t cpu_percent;     /* 0–100, averaged across both cores       */
    uint32_t uptime_ms;
} metrics_t;

void metrics_init(void);
void metrics_sample(metrics_t *out);
void metrics_print(int instances, const metrics_t *m);
