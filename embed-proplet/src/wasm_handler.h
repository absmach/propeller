#ifndef WASM_HANDLER_H
#define WASM_HANDLER_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/**
 * Loads and instantiates a Wasm module, then invokes its "main" function (if present).
 * The module remains loaded in memory so that it can be stopped later by ID.
 *
 * @param task_id     Unique identifier (string) for this Wasm "task."
 * @param wasm_data   Pointer to the Wasm file data in memory.
 * @param wasm_size   Size of the Wasm file data in bytes.
 * @param inputs      Array of 64-bit inputs that the Wasm main function might consume.
 * @param inputs_count Number of elements in the 'inputs' array.
 */
void execute_wasm_module(const char *task_id,
                         const uint8_t *wasm_data,
                         size_t wasm_size,
                         const uint64_t *inputs,
                         size_t inputs_count);

/**
 * Stops the Wasm module with the given task_id by deinstantiating and unloading it from memory.
 *
 * @param task_id  The unique string ID assigned to the Wasm module at start time.
 */
void stop_wasm_app(const char *task_id);

#ifdef __cplusplus
}
#endif

#endif /* WASM_HANDLER_H */
