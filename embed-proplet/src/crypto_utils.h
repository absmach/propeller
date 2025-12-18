/*
 * Copyright (c) Abstract Machines
 * SPDX-License-Identifier: Apache-2.0
 */

#ifndef CRYPTO_UTILS_H
#define CRYPTO_UTILS_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/**
 * @brief Decrypts an AES-256-GCM payload.
 *
 * The payload is expected to follow the standard Go crypto/cipher format:
 * [ Nonce (12 bytes) | Ciphertext (n bytes) | Tag (16 bytes) ]
 *
 * @param enc_data      Pointer to the full encrypted buffer.
 * @param enc_len       Total length of enc_data.
 * @param key           32-byte AES key (raw bytes).
 * @param dec_data      Output buffer for plaintext. Must be large enough (enc_len - 28).
 * @param dec_len       Pointer to store the actual size of decrypted data.
 * @return 0 on success, negative error code on failure.
 */
int decrypt_payload(const uint8_t *enc_data, size_t enc_len, const uint8_t *key,
                    uint8_t *dec_data, size_t *dec_len);

#ifdef __cplusplus
}
#endif

#endif /* CRYPTO_UTILS_H */
