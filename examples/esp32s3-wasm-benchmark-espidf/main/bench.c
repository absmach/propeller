#include "bench.h"
#include "metrics.h"
#include "wasm_bins.h"
#include "wasm_tasks.h"

#include <stdio.h>
#include <string.h>
#include <pthread.h>

#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "esp_pthread.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "wasm_export.h"

/* ──────────────────────────────────────────────────────────────────────────
 * Constants
 * ─────────────────────────────────────────────────────────────────────────*/
#define TAG              "bench"
#define MAX_INSTANCES    32
#define ERROR_BUF_SIZE   128

/* WAMR module loaded once, shared across all instances */
static wasm_module_t    s_shared_module  = NULL;
static bench_instance_t s_instances[MAX_INSTANCES];
static int              s_instance_count = 0;

/* ──────────────────────────────────────────────────────────────────────────
 * Workload data access
 * ─────────────────────────────────────────────────────────────────────────*/
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

/* ──────────────────────────────────────────────────────────────────────────
 * Per-instance pthread worker
 *
 * WAMR requires that any thread calling wasm_runtime_* APIs was created
 * with pthread_create (it uses pthread_self() internally for thread-local
 * state).  xTaskCreate threads do NOT satisfy this requirement.
 * ─────────────────────────────────────────────────────────────────────────*/
static void *wasm_worker_thread(void *arg)
{
    bench_instance_t *inst = (bench_instance_t *)arg;
    char err[ERROR_BUF_SIZE];

    /* Instantiate from the module assigned to this instance.
     * For the homogeneous bench this is s_shared_module; for the diverse bench
     * it is a task-specific module set in spawn_instance(). */
    inst->module_inst = wasm_runtime_instantiate(
        (wasm_module_t)inst->parent_module,
        inst->wasm_stack_bytes,  /* WAMR interpreter stack */
        0,                       /* heap_size=0: workloads don't use WASM malloc */
        err, sizeof(err));

    if (!inst->module_inst) {
        ESP_LOGW(TAG, "[%d] instantiate failed: %s", inst->id, err);
        inst->errors++;
        inst->alive = false;
        return NULL;
    }

    inst->exec_env = wasm_runtime_create_exec_env(
        (wasm_module_inst_t)inst->module_inst,
        inst->wasm_stack_bytes);

    if (!inst->exec_env) {
        ESP_LOGW(TAG, "[%d] create_exec_env failed", inst->id);
        inst->errors++;
        wasm_runtime_deinstantiate((wasm_module_inst_t)inst->module_inst);
        inst->module_inst = NULL;
        inst->alive = false;
        return NULL;
    }

    wasm_function_inst_t main_fn =
        wasm_runtime_lookup_function((wasm_module_inst_t)inst->module_inst, "main");
    if (!main_fn)
        main_fn = wasm_runtime_lookup_function((wasm_module_inst_t)inst->module_inst, "_start");

    if (!main_fn) {
        ESP_LOGW(TAG, "[%d] no main/_start export", inst->id);
        inst->errors++;
        goto done;
    }

    /* ── Run loop ── */
    while (inst->running) {
        uint64_t t0 = (uint64_t)esp_timer_get_time();

        uint32_t argv[2] = {0, 0};
        bool ok = wasm_runtime_call_wasm(
            (wasm_exec_env_t)inst->exec_env, main_fn, 2, argv);

        inst->last_latency_us = (uint32_t)(esp_timer_get_time() - t0);

        if (!ok) {
            const char *ex = wasm_runtime_get_exception(
                (wasm_module_inst_t)inst->module_inst);
            ESP_LOGW(TAG, "[%d] exception: %s", inst->id, ex ? ex : "?");
            wasm_runtime_clear_exception((wasm_module_inst_t)inst->module_inst);
            inst->errors++;
        } else {
            inst->iterations++;
        }

        /* Block for 1 tick so FreeRTOS IDLE can run and reset the watchdog.
         * sched_yield() only yields to equal-priority tasks and does NOT
         * give time to IDLE (lower priority), causing task-WDT triggers. */
        vTaskDelay(pdMS_TO_TICKS(5));
    }

done:
    wasm_runtime_destroy_exec_env((wasm_exec_env_t)inst->exec_env);
    wasm_runtime_deinstantiate((wasm_module_inst_t)inst->module_inst);
    inst->exec_env    = NULL;
    inst->module_inst = NULL;
    inst->alive       = false;
    return NULL;
}

/* ──────────────────────────────────────────────────────────────────────────
 * Spawn a single instance
 *
 * module: the wasm_module_t to instantiate from (cast to void* for header
 *         independence).  Pass NULL to use s_shared_module (homogeneous bench).
 * name:   human-readable task name for the stats table.  NULL → workload_name.
 * ─────────────────────────────────────────────────────────────────────────*/
static bool spawn_instance(int idx, const bench_config_t *cfg,
                           void *module, const char *name)
{
    bench_instance_t *inst = &s_instances[idx];
    memset(inst, 0, sizeof(*inst));

    inst->id               = idx;
    inst->workload         = cfg->workload;
    inst->running          = true;
    inst->alive            = true;
    inst->wasm_stack_bytes = cfg->wasm_stack_kb * 1024;
    inst->parent_module    = module ? module : (void *)s_shared_module;
    inst->task_name        = name;

    pthread_attr_t attr;
    pthread_attr_init(&attr);
    pthread_attr_setdetachstate(&attr, PTHREAD_CREATE_JOINABLE);
    pthread_attr_setstacksize(&attr, cfg->task_stack_kb * 1024);

    /* Set worker thread priority BELOW the benchmark harness (tskIDLE_PRIORITY+2=2)
     * so the harness task is never starved while worker initialises. */
    esp_pthread_cfg_t pcfg = esp_pthread_get_default_config();
    pcfg.prio = tskIDLE_PRIORITY + 1;   /* priority 1, below harness at 2 */
    esp_pthread_set_cfg(&pcfg);

    int rc = pthread_create(&inst->thread, &attr, wasm_worker_thread, inst);
    pthread_attr_destroy(&attr);

    if (rc != 0) {
        ESP_LOGW(TAG, "[%d] pthread_create failed: %d", idx, rc);
        inst->alive = false;
        return false;
    }
    return true;
}

/* ──────────────────────────────────────────────────────────────────────────
 * Stop and join all running instances
 * ─────────────────────────────────────────────────────────────────────────*/
static void stop_all_instances(void)
{
    for (int i = 0; i < s_instance_count; i++)
        s_instances[i].running = false;

    for (int i = 0; i < s_instance_count; i++) {
        if (s_instances[i].thread) {
            pthread_join(s_instances[i].thread, NULL);
            s_instances[i].thread = 0;
        }
    }
    s_instance_count = 0;
}

/* ──────────────────────────────────────────────────────────────────────────
 * Print per-instance detail table
 * ─────────────────────────────────────────────────────────────────────────*/
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
    fflush(stdout);
}

/* ──────────────────────────────────────────────────────────────────────────
 * bench_config_default
 * ─────────────────────────────────────────────────────────────────────────*/
void bench_config_default(bench_config_t *cfg)
{
    cfg->mode             = MODE_SHARED_MODULE;
    cfg->workload         = WORKLOAD_CPU;
    cfg->wasm_stack_kb    = 4;
    cfg->wasm_heap_kb     = 8;
    cfg->task_stack_kb    = 8;   /* pthread stack – must fit WAMR call frames */
    cfg->core_affinity    = -1;
    cfg->scale_delay_ms   = 500;
    cfg->measure_delay_ms = 2000;
    cfg->max_instances    = MAX_INSTANCES;
}

/* ──────────────────────────────────────────────────────────────────────────
 * bench_run  –  the main scaling loop
 * ─────────────────────────────────────────────────────────────────────────*/
int bench_run(const bench_config_t *cfg)
{
    int peak_instances = 0;

    printf("\n=== WASM Stress Benchmark ===\n");
    printf("workload=%s  mode=%s  wasm_stack=%uKB  wasm_heap=%uKB\n",
           workload_name(cfg->workload),
           cfg->mode == MODE_SHARED_MODULE ? "shared_module" : "separate_runtime",
           (unsigned)cfg->wasm_stack_kb, (unsigned)cfg->wasm_heap_kb);
    printf("task_stack=%uKB  core=%d\n\n",
           (unsigned)cfg->task_stack_kb, cfg->core_affinity);
    fflush(stdout);

    /* Load WASM module once – shared by all instances.
     *
     * IMPORTANT: wasm_runtime_load() (fast-interp mode) modifies the bytecode
     * buffer in-place during loading.  Our wasm_*[] arrays are 'static const'
     * and live in read-only DROM (flash MMU mapping).  Writing to DROM causes
     * a LoadStoreError exception.  We must copy to writable heap-DRAM first. */
    uint32_t wasm_len = 0;
    const uint8_t *wasm_ro = workload_bytes(cfg->workload, &wasm_len);
    if (!wasm_ro || wasm_len == 0) {
        ESP_LOGE(TAG, "No WASM bytes for workload %d", cfg->workload);
        return 0;
    }
    uint8_t *wasm_bytes = heap_caps_malloc(wasm_len, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!wasm_bytes) {
        ESP_LOGE(TAG, "malloc(%u) for WASM buffer failed", (unsigned)wasm_len);
        return 0;
    }
    memcpy(wasm_bytes, wasm_ro, wasm_len);

    char err[ERROR_BUF_SIZE];
    s_shared_module = wasm_runtime_load(wasm_bytes, wasm_len, err, sizeof(err));

    /* Note: wasm_bytes ownership transferred to WAMR; do not free it ourselves */
    if (!s_shared_module) {
        ESP_LOGE(TAG, "wasm_runtime_load failed: %s", err);
        free(wasm_bytes);
        return 0;
    }

    s_instance_count = 0;
    metrics_init();

    /* ── Scaling loop ── */
    for (int n = 1; n <= cfg->max_instances; n++) {
        metrics_t m_before;
        metrics_sample(&m_before);
        uint32_t heap_before = m_before.heap_free;

        /* Cost estimate per instance */
        uint32_t est_kb = cfg->wasm_stack_kb * 3   /* stack + heap + exec_env */
                        + cfg->task_stack_kb
                        + 8;                        /* WAMR overhead */

        if (heap_before < (est_kb + 16) * 1024) {
            printf("instances=%-3d  OOM (heap=%uKB < ~%uKB needed)\n",
                   n, (unsigned)(heap_before / 1024), (unsigned)(est_kb + 16));
            break;
        }

        if (!spawn_instance(s_instance_count, cfg, (void *)s_shared_module,
                            workload_name(cfg->workload))) {
            printf("instances=%-3d  SPAWN_FAIL\n", n);
            break;
        }
        s_instance_count++;
        peak_instances = s_instance_count;

        /* Let instance settle and instantiate WAMR */
        vTaskDelay(pdMS_TO_TICKS(cfg->scale_delay_ms));

        if (!s_instances[s_instance_count - 1].alive) {
            printf("instances=%-3d  TASK_DIED (errors=%u)\n",
                   n, (unsigned)s_instances[s_instance_count - 1].errors);
            s_instance_count--;
            peak_instances = s_instance_count;
            break;
        }

        vTaskDelay(pdMS_TO_TICKS(cfg->measure_delay_ms));

        metrics_t m;
        metrics_sample(&m);
        metrics_print(s_instance_count, &m);

        uint32_t delta = (heap_before > m.heap_free) ?
                         (heap_before - m.heap_free) : 0;
        printf("  +instance cost ~%uKB  latency %uus\n",
               (unsigned)(delta / 1024),
               (unsigned)s_instances[s_instance_count - 1].last_latency_us);
        fflush(stdout);
    }

    /* Final snapshot */
    vTaskDelay(pdMS_TO_TICKS(cfg->measure_delay_ms));
    printf("\n--- Peak: %d concurrent WASM instances ---\n", peak_instances);
    print_instance_stats();

    stop_all_instances();
    wasm_runtime_unload(s_shared_module);
    s_shared_module = NULL;

    metrics_t m_final;
    metrics_sample(&m_final);
    printf("Post-teardown heap: %uKB free\n\n",
           (unsigned)(m_final.heap_free / 1024));
    fflush(stdout);

    return peak_instances;
}

/* ──────────────────────────────────────────────────────────────────────────
 * bench_run_all_workloads
 * ─────────────────────────────────────────────────────────────────────────*/
void bench_run_all_workloads(const bench_config_t *base_cfg)
{
    int results[WORKLOAD_COUNT];
    bench_config_t cfg;

    printf("\n╔══════════════════════════════════════════╗\n");
    printf("║  WORKLOAD COMPARISON                     ║\n");
    printf("╚══════════════════════════════════════════╝\n\n");
    fflush(stdout);

    for (int w = 0; w < WORKLOAD_COUNT; w++) {
        memcpy(&cfg, base_cfg, sizeof(cfg));
        cfg.workload = (workload_type_t)w;
        results[w] = bench_run(&cfg);
        vTaskDelay(pdMS_TO_TICKS(2000));
    }

    printf("\n╔══════════════════════════════════════════╗\n");
    printf("║  SUMMARY                                 ║\n");
    printf("╠══════════════════════════════════════════╣\n");
    for (int w = 0; w < WORKLOAD_COUNT; w++) {
        printf("║  %-8s  max_instances = %-3d           ║\n",
               workload_name((workload_type_t)w), results[w]);
    }
    printf("╚══════════════════════════════════════════╝\n\n");
    fflush(stdout);
}

/* ──────────────────────────────────────────────────────────────────────────
 * bench_compare_modes
 * ─────────────────────────────────────────────────────────────────────────*/
void bench_compare_modes(const bench_config_t *base_cfg)
{
    bench_config_t cfg;

    printf("\n╔══════════════════════════════════════════╗\n");
    printf("║  MODE COMPARISON (workload: cpu)         ║\n");
    printf("╚══════════════════════════════════════════╝\n\n");
    fflush(stdout);

    memcpy(&cfg, base_cfg, sizeof(cfg));
    cfg.mode = MODE_SHARED_MODULE;
    int shared = bench_run(&cfg);

    printf("Shared module: %d instances\n", shared);
    fflush(stdout);
}

/* ──────────────────────────────────────────────────────────────────────────
 * bench_run_diverse  –  five distinct WASM tasks running concurrently
 *
 * Loads five separate WASM modules (add, mul, fib, checksum, popcount) and
 * scales up by spawning one additional instance of every task per round,
 * until OOM or the instance cap is reached.
 * ─────────────────────────────────────────────────────────────────────────*/

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
    printf("wasm_stack=%uKB  task_stack=%uKB\n\n",
           (unsigned)cfg->wasm_stack_kb, (unsigned)cfg->task_stack_kb);
    fflush(stdout);

    /* ── Load all five modules ── */
    for (int t = 0; t < DIVERSE_TASK_COUNT; t++) {
        bufs[t] = heap_caps_malloc(s_task_defs[t].len,
                                   MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
        if (!bufs[t]) {
            ESP_LOGE(TAG, "malloc failed for task %s", s_task_defs[t].name);
            goto cleanup;
        }
        memcpy(bufs[t], s_task_defs[t].bytes_ro, s_task_defs[t].len);

        modules[t] = wasm_runtime_load(bufs[t], s_task_defs[t].len,
                                       err, sizeof(err));
        if (!modules[t]) {
            ESP_LOGE(TAG, "wasm_runtime_load failed for %s: %s",
                     s_task_defs[t].name, err);
            /* bufs[t] not yet owned by WAMR – free it */
            free(bufs[t]);
            bufs[t] = NULL;
            goto cleanup;
        }
        /* WAMR owns bufs[t] from here; cleared so cleanup won't double-free */
        bufs[t] = NULL;
        loaded = t + 1;
        ESP_LOGI(TAG, "Loaded module: %s (%u bytes)",
                 s_task_defs[t].name, (unsigned)s_task_defs[t].len);
    }

    s_instance_count = 0;
    metrics_init();

    int peak_sets = 0;
    int max_sets  = cfg->max_instances / DIVERSE_TASK_COUNT;

    /* ── Scaling loop: add one full set of 5 instances per round ── */
    for (int set_n = 1; set_n <= max_sets; set_n++) {

        metrics_t m_before;
        metrics_sample(&m_before);

        /* Conservative OOM pre-check:
         *   4 tasks × ~16 KB (no linear mem) + 1 task × ~88 KB (checksum)
         *   + 5 × task_stack  + safety margin */
        uint32_t est_kb = (4 * (cfg->wasm_stack_kb + cfg->task_stack_kb + 8))
                        + (cfg->wasm_stack_kb + cfg->task_stack_kb + 8 + 64)
                        + 24;   /* safety */
        if (m_before.heap_free < est_kb * 1024) {
            printf("set=%d  OOM (heap=%uKB < ~%uKB needed for next set)\n",
                   set_n, (unsigned)(m_before.heap_free / 1024),
                   (unsigned)est_kb);
            break;
        }

        /* Spawn one instance of each task */
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
        if (!ok) break;

        /* Wait for all new instances to finish instantiation */
        vTaskDelay(pdMS_TO_TICKS(cfg->scale_delay_ms));

        /* Check all newly spawned instances are alive */
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

        vTaskDelay(pdMS_TO_TICKS(cfg->measure_delay_ms));

        /* ── Print metrics for this set size ── */
        metrics_t m;
        metrics_sample(&m);

        printf("set_size=%-2d  total=%-3d  heap=%4uKB  cpu=%3u%%  up=%us\n",
               set_n, s_instance_count,
               (unsigned)(m.heap_free / 1024),
               (unsigned)m.cpu_percent,
               (unsigned)(m.uptime_ms / 1000));

        /* Per-task latency: average across all instances of that task.
         * Instances are laid out as [t0,t1,t2,t3,t4, t0,t1,...] so task t
         * occupies indices t, t+5, t+10, ... */
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
        fflush(stdout);
        peak_sets = set_n;
    }

    /* ── Final snapshot ── */
    vTaskDelay(pdMS_TO_TICKS(cfg->measure_delay_ms));
    printf("\n--- Peak: %d concurrent sets  (%d total WASM instances) ---\n",
           peak_sets, peak_sets * DIVERSE_TASK_COUNT);
    print_instance_stats();

    stop_all_instances();

    metrics_t m_final;
    metrics_sample(&m_final);
    printf("Post-teardown heap: %uKB free\n\n",
           (unsigned)(m_final.heap_free / 1024));
    fflush(stdout);

cleanup:
    for (int t = 0; t < loaded; t++) {
        if (modules[t]) wasm_runtime_unload(modules[t]);
    }
    /* bufs[] are either owned by WAMR (NULL'd above) or never loaded (free them) */
    for (int t = 0; t < DIVERSE_TASK_COUNT; t++) {
        if (bufs[t]) free(bufs[t]);
    }
}
