# Auth Keys

This directory should contain Ed25519 private keys for SuperMQ authentication.

**Do NOT commit private keys to version control.**

## Generate keys

```bash
openssl genpkey -algorithm Ed25519 -out active.key
openssl genpkey -algorithm Ed25519 -out retiring.key
```
