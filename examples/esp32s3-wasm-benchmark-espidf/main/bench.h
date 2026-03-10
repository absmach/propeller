#pragma once
#include <stdint.h>
#include <stdbool.h>
#include <pthread.h>
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

/* ── Workload types ─────────────────────────────────────────────────────── */
typedef enum {
    WORKLOAD_CPU = 0,   /* FNV-1a hash loop   */
    WORKLOAD_MEM,       /* linear-memory r/w   */
    WORKLOAD_MSG,       /* ring-buffer msg pass */
    WORKLOAD_COUNT
} workload_type_t;

/* ── Per-instance state (one pthread per WASM instance) ─────────────────── */
typedef struct {
    int              id;              /* instance index, 0-based         */
    workload_type_t  workload;        /* which WASM binary to run        */
    volatile bool    running;         /* set false to request shutdown   */
    volatile bool    alive;           /* cleared by thread on exit       */
    uint32_t         iterations;      /* completed wasm_execute calls    */
    uint32_t         errors;          /* wasm_execute failures           */
    uint32_t         last_latency_us; /* µs for last execute             */
    uint32_t         wasm_stack_bytes;/* WAMR interpreter stack size     */

    pthread_t        thread;          /* pthread handle                  */

    /* Set before spawn to override the shared module (used by diverse bench) */
    void            *parent_module;   /* wasm_module_t to instantiate from */
    const char      *task_name;       /* display name (overrides workload_name) */

    /* WAMR handles – set up inside the thread, torn down on exit */
    void            *module_inst;     /* wasm_module_inst_t              */
    void            *exec_env;        /* wasm_exec_env_t                 */
} bench_instance_t;

/* ── Bench experiment modes ─────────────────────────────────────────────── */
typedef enum {
    MODE_SEPARATE_RUNTIME = 0,  /* reinitialise WAMR per instance (expensive) */
    MODE_SHARED_MODULE,         /* shared module_t, separate instances         */
} bench_mode_t;

/* ── Configuration passed to bench_run() ───────────────────────────────── */
typedef struct {
    bench_mode_t     mode;
    workload_type_t  workload;         /* workload type for all instances  */
    uint32_t         wasm_stack_kb;    /* WAMR interpreter stack KB        */
    uint32_t         wasm_heap_kb;     /* WAMR instance heap KB            */
    uint32_t         task_stack_kb;    /* pthread stack KB                 */
    int              core_affinity;    /* 0, 1, or -1 for no affinity      */
    uint32_t         scale_delay_ms;   /* ms between adding instances      */
    uint32_t         measure_delay_ms; /* ms to measure at each step       */
    int              max_instances;    /* hard cap (0 = unlimited)         */
} bench_config_t;

/**
 * Default configuration – call before customising.
 */
void bench_config_default(bench_config_t *cfg);

/**
 * Run the full auto-scaling experiment.
 * Returns the peak number of concurrently running instances.
 */
int bench_run(const bench_config_t *cfg);

/**
 * Run all three workloads sequentially and print a comparison table.
 */
void bench_run_all_workloads(const bench_config_t *base_cfg);

/**
 * Compare shared-module mode across core affinity settings.
 */
void bench_compare_modes(const bench_config_t *base_cfg);

/**
 * Run five distinct WASM tasks (add, mul, fib, checksum, popcount) concurrently,
 * scaling up by one full set of five instances at a time until OOM.
 * Each task has its own separately loaded module.
 */
void bench_run_diverse(const bench_config_t *cfg);
