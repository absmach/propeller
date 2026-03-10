#pragma once

#include <stdbool.h>
#include <stdint.h>
#include <zephyr/kernel.h>

/* ── Workload types ─────────────────────────────────────────────────────── */
typedef enum {
    WORKLOAD_CPU = 0,   /* FNV-1a hash loop    */
    WORKLOAD_MEM,       /* linear-memory r/w   */
    WORKLOAD_MSG,       /* ring-buffer msg pass */
    WORKLOAD_COUNT
} workload_type_t;

/* ── Per-instance state ─────────────────────────────────────────────────── */
typedef struct {
    int              id;
    workload_type_t  workload;
    volatile bool    running;         /* set false to request shutdown   */
    volatile bool    alive;           /* cleared by thread on exit       */
    uint32_t         iterations;
    uint32_t         errors;
    uint32_t         last_latency_us;
    uint32_t         wasm_stack_bytes;

    struct k_thread  thread_data;     /* Zephyr thread object            */
    k_tid_t          tid;

    void            *parent_module;   /* wasm_module_t to instantiate from */
    const char      *task_name;

    void            *module_inst;     /* wasm_module_inst_t              */
    void            *exec_env;        /* wasm_exec_env_t                 */
} bench_instance_t;

/* ── Bench experiment modes ─────────────────────────────────────────────── */
typedef enum {
    MODE_SHARED_MODULE = 0,
} bench_mode_t;

/* ── Configuration ──────────────────────────────────────────────────────── */
typedef struct {
    bench_mode_t     mode;
    workload_type_t  workload;
    uint32_t         wasm_stack_kb;
    uint32_t         wasm_heap_kb;
    uint32_t         task_stack_kb;   /* Zephyr thread stack KB — capped at
                                         BENCH_MAX_STACK_KB at build time  */
    int              core_affinity;   /* 0, 1, or -1 for no preference    */
    uint32_t         scale_delay_ms;
    uint32_t         measure_delay_ms;
    int              max_instances;
} bench_config_t;

void bench_config_default(bench_config_t *cfg);
int  bench_run(const bench_config_t *cfg);
void bench_run_all_workloads(const bench_config_t *base_cfg);
void bench_compare_modes(const bench_config_t *base_cfg);
void bench_run_diverse(const bench_config_t *cfg);
