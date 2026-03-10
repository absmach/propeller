/*
 * ESP32-S3 WASM Stress Benchmark — Zephyr port
 * =============================================
 * Measures maximum concurrent WASM instances using WAMR under Zephyr RTOS.
 *
 * Memory layout (approximate, 423 KB usable DRAM):
 *   Zephyr kernel + BSS      ~100 KB
 *   Thread stacks (16 × 4KB)   64 KB  (static, not in malloc arena)
 *   Malloc arena (WAMR)        192 KB  (CONFIG_COMMON_LIBC_MALLOC_ARENA_SIZE)
 *
 * The heap pool is where all WAMR allocations land; heap_free reported
 * during the run reflects remaining capacity for new instances.
 *
 * Edit EXPERIMENT to select which test to run.
 * Build: west build -b esp32s3_devkitc/esp32s3/procpu -p auto .
 * Flash: west flash -p /dev/ttyUSB0
 */

#include <stdio.h>
#include <string.h>
#include <stdlib.h>

#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>
#include "wasm_export.h"

#include "bench.h"
#include "metrics.h"

LOG_MODULE_REGISTER(main);

/* ──────────────────────────────────────────────────────────────────────────
 * Experiment selector  –  edit EXPERIMENT to choose what to run
 *
 *  0  Full comparison of all three workload types
 *  1  Single CPU-stress scaling run (fastest to start)
 *  2  Single MEM-stress scaling run
 *  3  Single MSG-stress scaling run
 *  4  Core-pinned experiment: all on core 0 vs all on core 1 vs distributed
 *  5  Mode comparison (shared module)
 *  6  Diverse workload: 5 distinct tasks (add/mul/fib/checksum/popcount)
 * ─────────────────────────────────────────────────────────────────────────*/
#define EXPERIMENT 0

/* ──────────────────────────────────────────────────────────────────────────
 * Memory budget per instance
 *
 *   wasm_stack_kb  4   WAMR interpreter operand stack
 *   wasm_heap_kb   8   WASM linear memory (workloads without memory section
 *                      don't actually allocate this; set to 0 for those)
 * Thread stacks are pre-allocated statically (BENCH_THREAD_STACK_SIZE = 4 KB).
 * ─────────────────────────────────────────────────────────────────────────*/
#define WASM_STACK_KB  4
#define WASM_HEAP_KB   8

static void wamr_global_init(void)
{
    RuntimeInitArgs init_args;

    memset(&init_args, 0, sizeof(init_args));

    /* Use system allocator so all WAMR allocations are visible in heap stats */
    init_args.mem_alloc_type = Alloc_With_Allocator;
    init_args.mem_alloc_option.allocator.malloc_func  = (void *)malloc;
    init_args.mem_alloc_option.allocator.realloc_func = (void *)realloc;
    init_args.mem_alloc_option.allocator.free_func    = (void *)free;

    if (!wasm_runtime_full_init(&init_args)) {
        LOG_ERR("WAMR global init failed – halting");
        while (1) {
            k_sleep(K_FOREVER);
        }
    }
    LOG_INF("WAMR initialised");
}

static void print_system_info(void)
{
    metrics_t m;

    metrics_sample(&m);

    printf("\n========================================\n");
    printf("  ESP32-S3 WASM Benchmark (Zephyr)\n");
    printf("========================================\n");
    printf("Malloc arena   : %u KB  (CONFIG_COMMON_LIBC_MALLOC_ARENA_SIZE)\n",
           (unsigned)(CONFIG_COMMON_LIBC_MALLOC_ARENA_SIZE / 1024));
    printf("Heap free now  : %u KB\n", (unsigned)(m.heap_free / 1024));
    printf("Thread stacks  : %d slots × 4 KB = %u KB (static, not in arena)\n",
           24, (unsigned)(24 * 4));
    printf("Uptime         : %u ms\n", (unsigned)m.uptime_ms);
    printf("========================================\n\n");
}

int main(void)
{
    /* Give the system time to finish boot */
    k_sleep(K_MSEC(1000));

    wamr_global_init();
    print_system_info();

    bench_config_t cfg;

    bench_config_default(&cfg);
    cfg.wasm_stack_kb    = WASM_STACK_KB;
    cfg.wasm_heap_kb     = WASM_HEAP_KB;
    cfg.scale_delay_ms   = 300;
    cfg.measure_delay_ms = 2000;

#if EXPERIMENT == 0
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
    printf("\n--- Experiment: Core Pinning ---\n");
    printf("Run 1: All instances on Core 0\n");
    cfg.workload      = WORKLOAD_CPU;
    cfg.core_affinity = 0;
    int core0_max = bench_run(&cfg);
    k_sleep(K_MSEC(2000));

    printf("\nRun 2: All instances on Core 1\n");
    cfg.core_affinity = 1;
    int core1_max = bench_run(&cfg);
    k_sleep(K_MSEC(2000));

    printf("\nRun 3: Distributed (no affinity)\n");
    cfg.core_affinity = -1;
    int dist_max = bench_run(&cfg);

    printf("\n╔══════════════════════════════╗\n");
    printf("║  Pinning Summary             ║\n");
    printf("╠══════════════════════════════╣\n");
    printf("║  Core 0 only  : %-3d instances║\n", core0_max);
    printf("║  Core 1 only  : %-3d instances║\n", core1_max);
    printf("║  Distributed  : %-3d instances║\n", dist_max);
    printf("╚══════════════════════════════╝\n");

#elif EXPERIMENT == 5
    bench_compare_modes(&cfg);

#elif EXPERIMENT == 6
    bench_run_diverse(&cfg);
#endif

    printf("\nBenchmark complete. Halting.\n");
    wasm_runtime_destroy();

    while (1) {
        k_sleep(K_MSEC(5000));
        metrics_t m;

        metrics_sample(&m);
        printf("[idle] heap=%uKB  cpu=%u%%\n",
               (unsigned)(m.heap_free / 1024), (unsigned)m.cpu_percent);
    }

    return 0;
}
