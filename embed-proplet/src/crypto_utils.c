/*
 * Copyright (c) Abstract Machines
 * SPDX-License-Identifier: Apache-2.0
 */

#include "crypto_utils.h"
#include <mbedtls/gcm.h>
#include <string.h>
#include <zephyr/logging/log.h>

LOG_MODULE_REGISTER(crypto_utils, CONFIG_LOG_DEFAULT_LEVEL);

#define NONCE_SIZE 12
#define TAG_SIZE 16
#define KEY_SIZE_BITS 256
#define KEY_SIZE_BYTES (KEY_SIZE_BITS / 8)

int decrypt_payload(const uint8_t *enc_data, size_t enc_len, const uint8_t *key,
                    uint8_t *dec_data, size_t *dec_len)
{
    int ret;
    mbedtls_gcm_context gcm;

    if (enc_data == NULL || key == NULL || dec_data == NULL || dec_len == NULL) {
        LOG_ERR("Invalid arguments provided to decrypt_payload");
        return -1;
    }

    if (enc_len < (NONCE_SIZE + TAG_SIZE)) {
        LOG_ERR("Payload too short: %zu bytes (minimum %d)", enc_len, NONCE_SIZE + TAG_SIZE);
        return -1;
    }

    size_t ciphertext_len = enc_len - NONCE_SIZE - TAG_SIZE;
    *dec_len = ciphertext_len;

    const uint8_t *nonce = enc_data;
    const uint8_t *ciphertext = enc_data + NONCE_SIZE;
    const uint8_t *tag = enc_data + NONCE_SIZE + ciphertext_len;

    LOG_DBG("Decrypting: Total=%zu, Ciphertext=%zu, Nonce=%d, Tag=%d", 
            enc_len, ciphertext_len, NONCE_SIZE, TAG_SIZE);

    mbedtls_gcm_init(&gcm);

    ret = mbedtls_gcm_setkey(&gcm, MBEDTLS_CIPHER_ID_AES, key, KEY_SIZE_BITS);
    if (ret != 0) {
        LOG_ERR("mbedtls_gcm_setkey failed: -0x%04X", -ret);
        mbedtls_gcm_free(&gcm);
        return ret;
    }

    ret = mbedtls_gcm_auth_decrypt(&gcm,
                                   ciphertext_len,
                                   nonce, NONCE_SIZE,
                                   NULL, 0,
                                   tag, TAG_SIZE,
                                   ciphertext,
                                   dec_data);

    mbedtls_gcm_free(&gcm);

    if (ret != 0) {
        LOG_ERR("Decryption failed: -0x%04X. This usually means the Key is wrong or the data is corrupted.", -ret);
        return ret;
    }

    LOG_INF("Successfully decrypted %zu bytes", ciphertext_len);
    return 0;
}
