#include "bench.h"
#include "metrics.h"
#include "wasm_bins.h"
#include "wasm_tasks.h"

#include <stdio.h>
#include <string.h>
#include <stdlib.h>

#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>
#include "wasm_export.h"

LOG_MODULE_REGISTER(bench);

#define MAX_INSTANCES    24
#define ERROR_BUF_SIZE   128

#define BENCH_THREAD_STACK_SIZE (4 * 1024)

K_THREAD_STACK_ARRAY_DEFINE(s_stacks, MAX_INSTANCES, BENCH_THREAD_STACK_SIZE);

static wasm_module_t    s_shared_module  = NULL;
static bench_instance_t s_instances[MAX_INSTANCES];
static int              s_instance_count = 0;

static const uint8_t *workload_bytes(workload_type_t w, uint32_t *len)
{
    switch (w) {
    case WORKLOAD_CPU: *len = wasm_cpu_stress_len; return wasm_cpu_stress;
    case WORKLOAD_MEM: *len = wasm_mem_stress_len; return wasm_mem_stress;
    case WORKLOAD_MSG: *len = wasm_msg_stress_len; return wasm_msg_stress;
    default:           *len = 0;                   return NULL;
    }
}

static const char *workload_name(workload_type_t w)
{
    switch (w) {
    case WORKLOAD_CPU: return "cpu";
    case WORKLOAD_MEM: return "mem";
    case WORKLOAD_MSG: return "msg";
    default:           return "???";
    }
}

static void wasm_worker_entry(void *p1, void *p2, void *p3)
{
    bench_instance_t *inst = (bench_instance_t *)p1;
    char err[ERROR_BUF_SIZE];

    ARG_UNUSED(p2);
    ARG_UNUSED(p3);

    inst->module_inst = wasm_runtime_instantiate(
        (wasm_module_t)inst->parent_module,
        inst->wasm_stack_bytes,
        0,
        err, sizeof(err));

    if (!inst->module_inst) {
        LOG_WRN("[%d] instantiate failed: %s", inst->id, err);
        inst->errors++;
        inst->alive = false;
        return;
    }

    inst->exec_env = wasm_runtime_create_exec_env(
        (wasm_module_inst_t)inst->module_inst,
        inst->wasm_stack_bytes);

    if (!inst->exec_env) {
        LOG_WRN("[%d] create_exec_env failed", inst->id);
        inst->errors++;
        wasm_runtime_deinstantiate((wasm_module_inst_t)inst->module_inst);
        inst->module_inst = NULL;
        inst->alive = false;
        return;
    }

    wasm_function_inst_t main_fn =
        wasm_runtime_lookup_function((wasm_module_inst_t)inst->module_inst, "main");
    if (!main_fn) {
        main_fn = wasm_runtime_lookup_function(
            (wasm_module_inst_t)inst->module_inst, "_start");
    }

    if (!main_fn) {
        LOG_WRN("[%d] no main/_start export", inst->id);
        inst->errors++;
        goto done;
    }

    while (inst->running) {
        uint64_t t0 = k_cyc_to_us_floor64(k_cycle_get_64());

        uint32_t argv[2] = {0, 0};
        bool ok = wasm_runtime_call_wasm(
            (wasm_exec_env_t)inst->exec_env, main_fn, 2, argv);

        inst->last_latency_us = (uint32_t)(
            k_cyc_to_us_floor64(k_cycle_get_64()) - t0);

        if (!ok) {
            const char *ex = wasm_runtime_get_exception(
                (wasm_module_inst_t)inst->module_inst);
            LOG_WRN("[%d] exception: %s", inst->id, ex ? ex : "?");
            wasm_runtime_clear_exception(
                (wasm_module_inst_t)inst->module_inst);
            inst->errors++;
        } else {
            inst->iterations++;
        }

        k_sleep(K_MSEC(5));
    }

done:
    wasm_runtime_destroy_exec_env((wasm_exec_env_t)inst->exec_env);
    wasm_runtime_deinstantiate((wasm_module_inst_t)inst->module_inst);
    inst->exec_env    = NULL;
    inst->module_inst = NULL;
    inst->alive       = false;
}

static bool spawn_instance(int idx, const bench_config_t *cfg,
                           void *module, const char *name)
{
    if (idx >= MAX_INSTANCES) {
        LOG_WRN("instance index %d >= MAX_INSTANCES %d", idx, MAX_INSTANCES);
        return false;
    }

    bench_instance_t *inst = &s_instances[idx];
    memset(inst, 0, sizeof(*inst));

    inst->id               = idx;
    inst->workload         = cfg->workload;
    inst->running          = true;
    inst->alive            = true;
    inst->wasm_stack_bytes = cfg->wasm_stack_kb * 1024;
    inst->parent_module    = module ? module : (void *)s_shared_module;
    inst->task_name        = name;

    inst->tid = k_thread_create(
        &inst->thread_data,
        s_stacks[idx],
        BENCH_THREAD_STACK_SIZE,
        wasm_worker_entry,
        inst, NULL, NULL,
        K_LOWEST_APPLICATION_THREAD_PRIO,
        0,
        K_NO_WAIT);

    if (!inst->tid) {
        LOG_WRN("[%d] k_thread_create failed", idx);
        inst->alive = false;
        return false;
    }

#ifdef CONFIG_SCHED_CPU_MASK
    if (cfg->core_affinity >= 0) {
        k_thread_cpu_pin(inst->tid, cfg->core_affinity);
    }
#endif

    return true;
}

static void stop_all_instances(void)
{
    for (int i = 0; i < s_instance_count; i++) {
        s_instances[i].running = false;
    }
    for (int i = 0; i < s_instance_count; i++) {
        if (s_instances[i].tid) {
            k_thread_join(s_instances[i].tid, K_FOREVER);
            s_instances[i].tid = NULL;
        }
    }
    s_instance_count = 0;
}

static void print_instance_stats(void)
{
    printf("\n--- Instance detail ---\n");
    printf("  id  task        iters     errors  latency_us\n");
    for (int i = 0; i < s_instance_count; i++) {
        bench_instance_t *inst = &s_instances[i];
        const char *label = inst->task_name ? inst->task_name
                                             : workload_name(inst->workload);
        printf("  %-3d %-10s  %-8u  %-6u  %u\n",
               inst->id,
               label,
               (unsigned)inst->iterations,
               (unsigned)inst->errors,
               (unsigned)inst->last_latency_us);
    }
    printf("---\n\n");
}

void bench_config_default(bench_config_t *cfg)
{
    cfg->mode             = MODE_SHARED_MODULE;
    cfg->workload         = WORKLOAD_CPU;
    cfg->wasm_stack_kb    = 4;
    cfg->wasm_heap_kb     = 8;
    cfg->task_stack_kb    = 6;
    cfg->core_affinity    = -1;
    cfg->scale_delay_ms   = 500;
    cfg->measure_delay_ms = 2000;
    cfg->max_instances    = MAX_INSTANCES;
}

int bench_run(const bench_config_t *cfg)
{
    int peak_instances = 0;

    printf("\n=== WASM Stress Benchmark (Zephyr) ===\n");
    printf("workload=%s  wasm_stack=%uKB\n",
           workload_name(cfg->workload), (unsigned)cfg->wasm_stack_kb);
    printf("thread_stack=%uKB (pre-allocated, %d slots)  core=%d\n\n",
           (unsigned)(BENCH_THREAD_STACK_SIZE / 1024), MAX_INSTANCES,
           cfg->core_affinity);

    uint32_t wasm_len = 0;
    const uint8_t *wasm_ro = workload_bytes(cfg->workload, &wasm_len);

    if (!wasm_ro || wasm_len == 0) {
        LOG_ERR("No WASM bytes for workload %d", cfg->workload);
        return 0;
    }

    uint8_t *wasm_bytes = malloc(wasm_len);

    if (!wasm_bytes) {
        LOG_ERR("malloc(%u) for WASM buffer failed", (unsigned)wasm_len);
        return 0;
    }
    memcpy(wasm_bytes, wasm_ro, wasm_len);

    char err[ERROR_BUF_SIZE];

    s_shared_module = wasm_runtime_load(wasm_bytes, wasm_len, err, sizeof(err));

    if (!s_shared_module) {
        LOG_ERR("wasm_runtime_load failed: %s", err);
        free(wasm_bytes);
        return 0;
    }

    s_instance_count = 0;
    metrics_init();

    for (int n = 1; n <= cfg->max_instances; n++) {
        metrics_t m_before;

        metrics_sample(&m_before);

        if (!spawn_instance(s_instance_count, cfg,
                            (void *)s_shared_module,
                            workload_name(cfg->workload))) {
            printf("instances=%-3d  SPAWN_FAIL\n", n);
            break;
        }
        s_instance_count++;
        peak_instances = s_instance_count;

        k_sleep(K_MSEC(cfg->scale_delay_ms));

        if (!s_instances[s_instance_count - 1].alive) {
            printf("instances=%-3d  TASK_DIED (errors=%u)\n",
                   n, (unsigned)s_instances[s_instance_count - 1].errors);
            s_instance_count--;
            peak_instances = s_instance_count;
            break;
        }

        k_sleep(K_MSEC(cfg->measure_delay_ms));

        metrics_t m;

        metrics_sample(&m);
        metrics_print(s_instance_count, &m);

        uint32_t delta = (m_before.heap_free > m.heap_free) ?
                         (m_before.heap_free - m.heap_free) : 0;
        printf("  +instance cost ~%uKB  latency %uus\n",
               (unsigned)(delta / 1024),
               (unsigned)s_instances[s_instance_count - 1].last_latency_us);
    }

    k_sleep(K_MSEC(cfg->measure_delay_ms));
    printf("\n--- Peak: %d concurrent WASM instances ---\n", peak_instances);
    print_instance_stats();

    stop_all_instances();
    wasm_runtime_unload(s_shared_module);
    s_shared_module = NULL;

    metrics_t m_final;

    metrics_sample(&m_final);
    printf("Post-teardown heap: %uKB free\n\n",
           (unsigned)(m_final.heap_free / 1024));

    return peak_instances;
}

void bench_run_all_workloads(const bench_config_t *base_cfg)
{
    int results[WORKLOAD_COUNT];
    bench_config_t cfg;

    printf("\n╔══════════════════════════════════════════╗\n");
    printf("║  WORKLOAD COMPARISON                     ║\n");
    printf("╚══════════════════════════════════════════╝\n\n");

    for (int w = 0; w < WORKLOAD_COUNT; w++) {
        memcpy(&cfg, base_cfg, sizeof(cfg));
        cfg.workload = (workload_type_t)w;
        results[w] = bench_run(&cfg);
        k_sleep(K_MSEC(2000));
    }

    printf("\n╔══════════════════════════════════════════╗\n");
    printf("║  SUMMARY                                 ║\n");
    printf("╠══════════════════════════════════════════╣\n");
    for (int w = 0; w < WORKLOAD_COUNT; w++) {
        printf("║  %-8s  max_instances = %-3d           ║\n",
               workload_name((workload_type_t)w), results[w]);
    }
    printf("╚══════════════════════════════════════════╝\n\n");
}

void bench_compare_modes(const bench_config_t *base_cfg)
{
    bench_config_t cfg;

    printf("\n╔══════════════════════════════════════════╗\n");
    printf("║  MODE COMPARISON (workload: cpu)         ║\n");
    printf("╚══════════════════════════════════════════╝\n\n");

    memcpy(&cfg, base_cfg, sizeof(cfg));
    cfg.mode = MODE_SHARED_MODULE;
    int shared = bench_run(&cfg);

    printf("Shared module: %d instances\n", shared);
}

#define DIVERSE_TASK_COUNT 5

static const struct {
    const char    *name;
    const uint8_t *bytes_ro;
    uint32_t       len;
} s_task_defs[DIVERSE_TASK_COUNT] = {
    { "add",      wasm_add,      wasm_add_len      },
    { "mul",      wasm_mul,      wasm_mul_len      },
    { "fib",      wasm_fib,      wasm_fib_len      },
    { "checksum", wasm_checksum, wasm_checksum_len },
    { "popcount", wasm_popcount, wasm_popcount_len },
};

void bench_run_diverse(const bench_config_t *cfg)
{
    wasm_module_t modules[DIVERSE_TASK_COUNT];
    uint8_t      *bufs[DIVERSE_TASK_COUNT];
    char          err[ERROR_BUF_SIZE];
    int           loaded = 0;

    memset(modules, 0, sizeof(modules));
    memset(bufs,    0, sizeof(bufs));

    printf("\n╔══════════════════════════════════════════╗\n");
    printf("║  DIVERSE WORKLOAD BENCHMARK              ║\n");
    printf("║  5 tasks × N concurrent sets             ║\n");
    printf("╚══════════════════════════════════════════╝\n\n");
    printf("Tasks: add | mul | fib | checksum | popcount\n");
    printf("wasm_stack=%uKB  thread_stack=%uKB (pre-allocated)\n\n",
           (unsigned)cfg->wasm_stack_kb,
           (unsigned)(BENCH_THREAD_STACK_SIZE / 1024));

    for (int t = 0; t < DIVERSE_TASK_COUNT; t++) {
        bufs[t] = malloc(s_task_defs[t].len);
        if (!bufs[t]) {
            LOG_ERR("malloc failed for task %s", s_task_defs[t].name);
            goto cleanup;
        }
        memcpy(bufs[t], s_task_defs[t].bytes_ro, s_task_defs[t].len);

        modules[t] = wasm_runtime_load(bufs[t], s_task_defs[t].len,
                                       err, sizeof(err));
        if (!modules[t]) {
            LOG_ERR("wasm_runtime_load failed for %s: %s",
                    s_task_defs[t].name, err);
            free(bufs[t]);
            bufs[t] = NULL;
            goto cleanup;
        }
        bufs[t] = NULL;
        loaded = t + 1;
        LOG_INF("Loaded module: %s (%u bytes)",
                s_task_defs[t].name, (unsigned)s_task_defs[t].len);
    }

    s_instance_count = 0;
    metrics_init();

    int peak_sets = 0;
    int max_sets  = cfg->max_instances / DIVERSE_TASK_COUNT;

    for (int set_n = 1; set_n <= max_sets; set_n++) {
        bool ok = true;

        for (int t = 0; t < DIVERSE_TASK_COUNT && ok; t++) {
            if (!spawn_instance(s_instance_count, cfg,
                                (void *)modules[t], s_task_defs[t].name)) {
                printf("  SPAWN_FAIL  task=%s\n", s_task_defs[t].name);
                ok = false;
            } else {
                s_instance_count++;
            }
        }
        if (!ok) {
            break;
        }

        k_sleep(K_MSEC(cfg->scale_delay_ms));

        bool all_alive = true;

        for (int i = s_instance_count - DIVERSE_TASK_COUNT;
             i < s_instance_count; i++) {
            if (!s_instances[i].alive) {
                printf("  TASK_DIED  id=%d task=%s errors=%u\n",
                       s_instances[i].id,
                       s_instances[i].task_name,
                       (unsigned)s_instances[i].errors);
                all_alive = false;
            }
        }
        if (!all_alive) {
            s_instance_count -= DIVERSE_TASK_COUNT;
            break;
        }

        k_sleep(K_MSEC(cfg->measure_delay_ms));

        metrics_t m;

        metrics_sample(&m);
        printf("set_size=%-2d  total=%-3d  heap=%4uKB  cpu=%3u%%  up=%us\n",
               set_n, s_instance_count,
               (unsigned)(m.heap_free / 1024),
               (unsigned)m.cpu_percent,
               (unsigned)(m.uptime_ms / 1000));

        for (int t = 0; t < DIVERSE_TASK_COUNT; t++) {
            uint64_t lat_sum  = 0;
            uint32_t iter_sum = 0;
            uint32_t err_sum  = 0;
            int      count    = 0;

            for (int i = t; i < s_instance_count; i += DIVERSE_TASK_COUNT) {
                lat_sum  += s_instances[i].last_latency_us;
                iter_sum += s_instances[i].iterations;
                err_sum  += s_instances[i].errors;
                count++;
            }
            printf("  %-10s  latency=%6uus  iters=%-6u  errors=%u\n",
                   s_task_defs[t].name,
                   count ? (unsigned)(lat_sum / count) : 0,
                   (unsigned)iter_sum,
                   (unsigned)err_sum);
        }
        peak_sets = set_n;
    }

    k_sleep(K_MSEC(cfg->measure_delay_ms));
    printf("\n--- Peak: %d sets  (%d total WASM instances) ---\n",
           peak_sets, peak_sets * DIVERSE_TASK_COUNT);
    print_instance_stats();

    stop_all_instances();

    metrics_t m_final;

    metrics_sample(&m_final);
    printf("Post-teardown heap: %uKB free\n\n",
           (unsigned)(m_final.heap_free / 1024));

cleanup:
    for (int t = 0; t < loaded; t++) {
        if (modules[t]) {
            wasm_runtime_unload(modules[t]);
        }
    }
    for (int t = 0; t < DIVERSE_TASK_COUNT; t++) {
        if (bufs[t]) {
            free(bufs[t]);
        }
    }
}
