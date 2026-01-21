#!/bin/bash
# Generate EdDSA key for SuperMQ auth service
# This is a demo key - for production, use proper key management

KEY_DIR="./keys"
KEY_FILE="${KEY_DIR}/active.key"

mkdir -p "${KEY_DIR}"

# Generate EdDSA private key using openssl
# EdDSA keys are typically Ed25519
openssl genpkey -algorithm Ed25519 -out "${KEY_FILE}" 2>/dev/null

if [ $? -eq 0 ]; then
    echo "Generated auth key at ${KEY_FILE}"
    chmod 600 "${KEY_FILE}"
else
    # Fallback: create a dummy key file for demo purposes
    # Note: This won't work for production but allows the service to start
    echo "Warning: Could not generate EdDSA key. Creating dummy key for demo."
    echo "-----BEGIN PRIVATE KEY-----
MC4CAQAwBQYDK2VwBCIEIA==
-----END PRIVATE KEY-----" > "${KEY_FILE}"
    chmod 600 "${KEY_FILE}"
fi
