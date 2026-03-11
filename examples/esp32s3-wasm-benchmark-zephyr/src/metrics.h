#pragma once

#include <stdint.h>

typedef struct {
    uint32_t heap_free;
    uint32_t heap_min;
    uint32_t cpu_percent;
    uint32_t uptime_ms;
} metrics_t;

void metrics_init(void);
void metrics_sample(metrics_t *out);
void metrics_print(int instances, const metrics_t *m);
