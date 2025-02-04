#include "wasm_handler.h"
#include "mqtt_client.h"
#include <logging/log.h>
#include <stdbool.h>
#include <string.h>
#include <wasm_export.h>

LOG_MODULE_REGISTER(wasm_handler);

#define MAX_WASM_APPS 10
#define MAX_ID_LEN 64
#define MAX_INPUTS 16
#define MAX_RESULTS 16

typedef struct {
  bool in_use;
  char id[MAX_ID_LEN];
  wasm_module_t module;
  wasm_module_inst_t module_inst;
} wasm_app_t;

static wasm_app_t g_wasm_apps[MAX_WASM_APPS];

static bool g_wamr_initialized = false;

static void maybe_init_wamr_runtime(void);
static int find_free_slot(void);
static int find_app_by_id(const char *task_id);

void execute_wasm_module(const char *task_id, const uint8_t *wasm_data,
                         size_t wasm_size, const uint64_t *inputs,
                         size_t inputs_count) {
  maybe_init_wamr_runtime();
  if (!g_wamr_initialized) {
    LOG_ERR("WAMR runtime not available, cannot execute WASM");
    return;
  }

  int existing_idx = find_app_by_id(task_id);
  if (existing_idx >= 0) {
    LOG_WRN("WASM app with ID %s is already running. Stopping it first...",
            task_id);
    stop_wasm_app(task_id);
  }

  int slot = find_free_slot();
  if (slot < 0) {
    LOG_ERR("No free slot to store new WASM app instance (increase "
            "MAX_WASM_APPS).");
    return;
  }

  char error_buf[128];
  wasm_module_t module =
      wasm_runtime_load(wasm_data, wasm_size, error_buf, sizeof(error_buf));
  if (!module) {
    LOG_ERR("Failed to load WASM module: %s", error_buf);
    return;
  }

  wasm_module_inst_t module_inst =
      wasm_runtime_instantiate(module, 16 * 1024, /* stack size */
                               16 * 1024,         /* heap size */
                               error_buf, sizeof(error_buf));
  if (!module_inst) {
    LOG_ERR("Failed to instantiate WASM module: %s", error_buf);
    wasm_runtime_unload(module);
    return;
  }

  g_wasm_apps[slot].in_use = true;
  strncpy(g_wasm_apps[slot].id, task_id, MAX_ID_LEN - 1);
  g_wasm_apps[slot].id[MAX_ID_LEN - 1] = '\0';
  g_wasm_apps[slot].module = module;
  g_wasm_apps[slot].module_inst = module_inst;

  wasm_function_inst_t func = wasm_runtime_lookup_function(module_inst, "main");
  if (!func) {
    LOG_WRN(
        "Function 'main' not found in WASM module. No entry point to call.");
    wasm_runtime_deinstantiate(module_inst);
    wasm_runtime_unload(module);
    return;
  }

  uint32_t result_count = wasm_func_get_result_count(func, module_inst);
  if (result_count == 0) {
    LOG_ERR("Function has no return value.");
    wasm_runtime_deinstantiate(module_inst);
    wasm_runtime_unload(module);
    return;
  }

  wasm_valkind_t result_types[result_count];
  wasm_func_get_result_types(func, module_inst, result_types);

  wasm_val_t results[result_count];
  for (uint32_t i = 0; i < result_count; i++) {
    results[i].kind = result_types[i];
  }

  wasm_val_t args[MAX_INPUTS];
  size_t n_args = (inputs_count > MAX_INPUTS) ? MAX_INPUTS : inputs_count;
  for (size_t i = 0; i < n_args; i++) {
    args[i].kind = WASM_I32;
    args[i].of.i32 = (uint32_t)(inputs[i] & 0xFFFFFFFFu);
  }

  wasm_exec_env_t exec_env =
      wasm_runtime_create_exec_env(module_inst, 16 * 1024);
  if (!exec_env) {
    LOG_ERR("Failed to create execution environment for WASM module.");
    wasm_runtime_deinstantiate(module_inst);
    wasm_runtime_unload(module);
    return;
  }

  if (!wasm_runtime_call_wasm_a(exec_env, func, result_count, results, n_args,
                                args)) {
    const char *exception = wasm_runtime_get_exception(module_inst);
    LOG_ERR("Error invoking WASM function: %s",
            exception ? exception : "Unknown error");
  } else {
    char result_payload[256] = {0};
    char results_string[MAX_RESULTS * 16] = {0};

    for (uint32_t i = 0; i < result_count; i++) {
      if (results[i].kind == WASM_I32) {
        char temp[16];
        snprintf(temp, sizeof(temp), "%d", results[i].of.i32);
        strncat(results_string, temp,
                sizeof(results_string) - strlen(results_string) - 1);
        if (i < result_count - 1) {
          strncat(results_string, ",",
                  sizeof(results_string) - strlen(results_string) - 1);
        }
      }
    }

    extern const char *channel_id;
    snprintf(result_payload, sizeof(result_payload),
             "{\"task_id\":\"%s\",\"results\":[%s]}", task_id, results_string);
    publish_results(channel_id, task_id, result_payload);
    LOG_INF("WASM execution results published to MQTT topic");
  }

  wasm_runtime_destroy_exec_env(exec_env);
  wasm_runtime_deinstantiate(module_inst);
  wasm_runtime_unload(module);
}

void stop_wasm_app(const char *task_id) {
  int idx = find_app_by_id(task_id);
  if (idx < 0) {
    LOG_WRN("No running WASM app found with ID=%s", task_id);
    return;
  }

  wasm_app_t *app = &g_wasm_apps[idx];
  LOG_INF("Stopping WASM app with ID=%s", app->id);

  wasm_runtime_deinstantiate(app->module_inst);
  wasm_runtime_unload(app->module);

  app->in_use = false;
  memset(app->id, 0, sizeof(app->id));

  LOG_INF("WASM app [%s] has been stopped and unloaded.", task_id);
}

static void maybe_init_wamr_runtime(void) {
  if (g_wamr_initialized) {
    return;
  }

  RuntimeInitArgs init_args;
  memset(&init_args, 0, sizeof(init_args));
  init_args.mem_alloc_type = Alloc_With_System_Allocator;

  if (!wasm_runtime_full_init(&init_args)) {
    LOG_ERR("Failed to initialize WAMR runtime.");
    return;
  }

  g_wamr_initialized = true;
  LOG_INF("WAMR runtime initialized successfully.");
}

static int find_free_slot(void) {
  for (int i = 0; i < MAX_WASM_APPS; i++) {
    if (!g_wasm_apps[i].in_use) {
      return i;
    }
  }
  return -1;
}

static int find_app_by_id(const char *task_id) {
  for (int i = 0; i < MAX_WASM_APPS; i++) {
    if (g_wasm_apps[i].in_use && (strcmp(g_wasm_apps[i].id, task_id) == 0)) {
      return i;
    }
  }
  return -1;
}
