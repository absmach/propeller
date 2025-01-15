#ifndef WASM_HANDLER_H
#define WASM_HANDLER_H

#include <zephyr/kernel.h>

/**
 * Execute a Wasm application from the provided memory buffer.
 *
 * @param wasm_data Pointer to the Wasm file data in memory.
 * @param wasm_size Size of the Wasm file data in bytes.
 */
void execute_wasm_from_memory(const uint8_t *wasm_data, size_t wasm_size);

#endif /* WASM_HANDLER_H */
