#include "wasm_handler.h"
#include <wasm_export.h>
#include <logging/log.h>

LOG_MODULE_REGISTER(wasm_handler);

void execute_wasm_module(const uint8_t *wasm_data, size_t wasm_size)
{
    RuntimeInitArgs init_args = { .mem_alloc_type = Alloc_With_System_Allocator };
    if (!wasm_runtime_full_init(&init_args)) {
        LOG_ERR("Failed to initialize WAMR runtime.");
        return;
    }

    char error_buf[128];
    wasm_module_t module = wasm_runtime_load(wasm_data, wasm_size, error_buf, sizeof(error_buf));
    if (!module) {
        LOG_ERR("Failed to load Wasm module: %s", error_buf);
        wasm_runtime_destroy();
        return;
    }

    wasm_module_inst_t module_inst = wasm_runtime_instantiate(module, 1024, 1024, error_buf, sizeof(error_buf));
    if (!module_inst) {
        LOG_ERR("Failed to instantiate Wasm module: %s", error_buf);
        wasm_runtime_unload(module);
        wasm_runtime_destroy();
        return;
    }

    wasm_function_inst_t func = wasm_runtime_lookup_function(module_inst, "main");
    if (func) {
        LOG_INF("Executing Wasm application...");
        if (!wasm_runtime_call_wasm(module_inst, func, 0, NULL)) {
            LOG_ERR("Error invoking Wasm function.");
        } else {
            LOG_INF("Wasm application executed successfully.");
        }
    } else {
        LOG_ERR("Function 'main' not found in Wasm module.");
    }

    wasm_runtime_deinstantiate(module_inst);
    wasm_runtime_unload(module);
    wasm_runtime_destroy();
}
