# Auth Keys

This directory should contain Ed25519 private keys for SuperMQ authentication.

**Do NOT commit private keys to version control.**

## Generate keys

```bash
openssl genpkey -algorithm Ed25519 -out active.key
openssl genpkey -algorithm Ed25519 -out retiring.key
```

## Key rotation

If keys have ever been committed to version control, they must be considered compromised.
Regenerate them immediately using the commands above.
