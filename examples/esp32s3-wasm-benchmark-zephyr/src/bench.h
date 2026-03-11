#pragma once

#include <stdbool.h>
#include <stdint.h>
#include <zephyr/kernel.h>

typedef enum {
    WORKLOAD_CPU = 0,
    WORKLOAD_MEM,
    WORKLOAD_MSG,
    WORKLOAD_COUNT
} workload_type_t;

typedef struct {
    int              id;
    workload_type_t  workload;
    volatile bool    running;
    volatile bool    alive;
    uint32_t         iterations;
    uint32_t         errors;
    uint32_t         last_latency_us;
    uint32_t         wasm_stack_bytes;

    struct k_thread  thread_data;
    k_tid_t          tid;

    void            *parent_module;
    const char      *task_name;

    void            *module_inst;
    void            *exec_env;
} bench_instance_t;

typedef enum {
    MODE_SHARED_MODULE = 0,
} bench_mode_t;

typedef struct {
    bench_mode_t     mode;
    workload_type_t  workload;
    uint32_t         wasm_stack_kb;
    uint32_t         wasm_heap_kb;
    uint32_t         task_stack_kb;
    int              core_affinity;
    uint32_t         scale_delay_ms;
    uint32_t         measure_delay_ms;
    int              max_instances;
} bench_config_t;

void bench_config_default(bench_config_t *cfg);
int  bench_run(const bench_config_t *cfg);
void bench_run_all_workloads(const bench_config_t *base_cfg);
void bench_compare_modes(const bench_config_t *base_cfg);
void bench_run_diverse(const bench_config_t *cfg);
