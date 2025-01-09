#ifndef WASM_HANDLER_H
#define WASM_HANDLER_H

#include <zephyr/kernel.h>

/**
 * Handle a chunk of incoming Wasm file data.
 *
 * @param data Pointer to the data chunk.
 * @param len Length of the data chunk.
 */
void handle_wasm_chunk(const uint8_t *data, size_t len);

/**
 * Execute a Wasm application from the provided memory buffer.
 *
 * @param wasm_data Pointer to the Wasm file data in memory.
 * @param wasm_size Size of the Wasm file data in bytes.
 */
void execute_wasm_from_memory(const uint8_t *wasm_data, size_t wasm_size);

#endif /* WASM_HANDLER_H */
