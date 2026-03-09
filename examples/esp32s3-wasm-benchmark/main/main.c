/*
 * ESP32-S3 WASM Stress Benchmark
 * ================================
 * Measures maximum concurrent WASM workloads using WAMR interpreter.
 *
 * Serial output (115200 baud):
 *   instances=1    heap= 412KB  min= 410KB  cpu=  3%  up=3s
 *   instances=5    heap= 350KB  min= 345KB  cpu= 12%  up=13s
 *   instances=10   heap= 280KB  min= 275KB  cpu= 28%  up=28s
 *   ...
 *   instances=26   OOM (pre-check: heap=62KB < min required ~28KB)
 */

#include <stdio.h>
#include <string.h>

#include "esp_log.h"
#include "esp_heap_caps.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "wasm_export.h"

#include "bench.h"
#include "metrics.h"

#define TAG "main"

/* ──────────────────────────────────────────────────────────────────────────
 * Experiment selector  –  edit EXPERIMENT to choose what to run
 *
 *  0  Full comparison of all three workload types
 *  1  Single CPU-stress scaling run (fastest to start)
 *  2  Single MEM-stress scaling run
 *  3  Single MSG-stress scaling run
 *  4  Core-pinned experiment: all on core 0 vs all on core 1
 *  5  Cooperative yield vs busy loop comparison (see notes in bench.c)
 *  6  Diverse workload: 5 distinct tasks (add/mul/fib/checksum/popcount)
 *     each with its own separately loaded WASM module, scaled concurrently
 * ─────────────────────────────────────────────────────────────────────────*/
#define EXPERIMENT  6

/* ──────────────────────────────────────────────────────────────────────────
 * Tune memory budget per WASM instance.
 *
 * Decrease these to fit more instances:
 *   wasm_stack_kb  4   (WAMR interpreter operand stack)
 *   wasm_heap_kb   8   (WASM linear memory / heap for the module)
 *   task_stack_kb  6   (FreeRTOS task native stack)
 *
 * Each instance costs roughly:
 *   wasm_stack_kb + wasm_heap_kb + task_stack_kb + ~8KB WAMR overhead
 * ─────────────────────────────────────────────────────────────────────────*/
#define WASM_STACK_KB  4
#define WASM_HEAP_KB   8
#define TASK_STACK_KB  6

static void wamr_global_init(void)
{
    RuntimeInitArgs init_args;
    memset(&init_args, 0, sizeof(init_args));

    /* Use system allocator (malloc/free from DRAM heap) */
    init_args.mem_alloc_type = Alloc_With_Allocator;
    init_args.mem_alloc_option.allocator.malloc_func  = (void *)malloc;
    init_args.mem_alloc_option.allocator.realloc_func = (void *)realloc;
    init_args.mem_alloc_option.allocator.free_func    = (void *)free;

    if (!wasm_runtime_full_init(&init_args)) {
        ESP_LOGE(TAG, "WAMR global init failed – halting");
        while (1) vTaskDelay(portMAX_DELAY);
    }
    ESP_LOGI(TAG, "WAMR initialised");
}

static void print_system_info(void)
{
    multi_heap_info_t info;
    heap_caps_get_info(&info, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);

    printf("\n========================================\n");
    printf("  ESP32-S3 WASM Stress Benchmark\n");
    printf("========================================\n");
    printf("Internal DRAM:\n");
    printf("  total free  : %u KB\n", (unsigned)(info.total_free_bytes     / 1024));
    printf("  total alloc : %u KB\n", (unsigned)(info.total_allocated_bytes / 1024));
    printf("  largest blk : %u KB\n", (unsigned)(info.largest_free_block   / 1024));
    printf("  min free    : %u KB\n", (unsigned)(info.minimum_free_bytes   / 1024));

    uint32_t psram = heap_caps_get_free_size(MALLOC_CAP_SPIRAM);
    if (psram > 0) {
        printf("PSRAM free    : %u KB\n", (unsigned)(psram / 1024));
    } else {
        printf("PSRAM         : not detected\n");
    }
    printf("========================================\n\n");
    fflush(stdout);
}

static void benchmark_task(void *arg)
{
    (void)arg;
    /* Give the system time to finish boot */
    vTaskDelay(pdMS_TO_TICKS(1000));

    wamr_global_init();
    print_system_info();

    bench_config_t cfg;
    bench_config_default(&cfg);
    cfg.wasm_stack_kb    = WASM_STACK_KB;
    cfg.wasm_heap_kb     = WASM_HEAP_KB;
    cfg.task_stack_kb    = TASK_STACK_KB;
    cfg.scale_delay_ms   = 300;
    cfg.measure_delay_ms = 2000;

#if EXPERIMENT == 0
    /* ── All workloads comparison ── */
    bench_run_all_workloads(&cfg);

#elif EXPERIMENT == 1
    cfg.workload = WORKLOAD_CPU;
    bench_run(&cfg);

#elif EXPERIMENT == 2
    cfg.workload = WORKLOAD_MEM;
    bench_run(&cfg);

#elif EXPERIMENT == 3
    cfg.workload = WORKLOAD_MSG;
    bench_run(&cfg);

#elif EXPERIMENT == 4
    /* ── Core pinning comparison ── */
    printf("\n--- Experiment: Core Pinning ---\n");
    printf("Run 1: All instances on Core 0\n");
    cfg.workload       = WORKLOAD_CPU;
    cfg.core_affinity  = 0;
    int core0_max = bench_run(&cfg);
    vTaskDelay(pdMS_TO_TICKS(2000));

    printf("\nRun 2: All instances on Core 1\n");
    cfg.core_affinity  = 1;
    int core1_max = bench_run(&cfg);
    vTaskDelay(pdMS_TO_TICKS(2000));

    printf("\nRun 3: Distributed (no affinity)\n");
    cfg.core_affinity  = -1;
    int dist_max = bench_run(&cfg);

    printf("\n╔══════════════════════════════╗\n");
    printf("║  Pinning Summary             ║\n");
    printf("╠══════════════════════════════╣\n");
    printf("║  Core 0 only  : %-3d instances║\n", core0_max);
    printf("║  Core 1 only  : %-3d instances║\n", core1_max);
    printf("║  Distributed  : %-3d instances║\n", dist_max);
    printf("╚══════════════════════════════╝\n");
    fflush(stdout);

#elif EXPERIMENT == 5
    bench_compare_modes(&cfg);

#elif EXPERIMENT == 6
    bench_run_diverse(&cfg);
#endif

    printf("\nBenchmark complete. Halting.\n");
    fflush(stdout);
    wasm_runtime_destroy();

    while (1) {
        vTaskDelay(pdMS_TO_TICKS(5000));
        metrics_t m;
        metrics_sample(&m);
        printf("[idle] heap=%uKB  cpu=%u%%\n", (unsigned)(m.heap_free / 1024), (unsigned)m.cpu_percent);
        fflush(stdout);
    }
}

void app_main(void)
{
    /* Run benchmark from a dedicated task with a larger stack than the
       default app_main stack (8KB), so we have room for printf buffers etc. */
    xTaskCreatePinnedToCore(
        benchmark_task,
        "bench_main",
        16 * 1024,          /* 16KB native stack for the harness */
        NULL,
        tskIDLE_PRIORITY + 2,
        NULL,
        0                   /* pin harness to core 0 */
    );
}
