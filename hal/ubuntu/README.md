# HAL

Builds and runs an Ubuntu CVM with Propeller services pre-installed via QEMU.

For full documentation, see [propeller.absmach.eu](https://propeller.absmach.eu/docs/hal).

## Prerequisites

```bash
sudo apt-get update
sudo apt-get install -y \
  qemu-system-x86 \
  cloud-image-utils \
  ovmf \
  wget
```

Docker and Docker Compose are also required on the host to run Trustee, the Key Broker Service that releases decryption keys to the guest (see [Run Trustee on the host](#run-trustee-on-the-host)).

## Configure

Set these before running the script:

| Variable               | Description                                             | Default                |
| ---------------------- | ------------------------------------------------------- | ---------------------- |
| `PROPLET_TENANT_ID`    | Propeller tenant ID                                     |                        |
| `PROPLET_ENTITY_ID`    | Propeller entity ID                                     |                        |
| `PROPLET_API_KEY`      | Propeller entity key                                    |                        |
| `PROPLET_CHANNEL_ID`   | Propeller channel ID                                    |                        |
| `PROPLET_MQTT_ADDRESS` | MQTT broker address                                     | `tcp://localhost:1883` |
| `KBS_URL`              | Key Broker Service URL                                  | `http://10.0.2.2:8082` |
| `KBS_CERT_PATH`        | Path to a PEM cert to trust for an `https://` `KBS_URL` |                        |
| `ENABLE_CVM`           | `auto`, `tdx`, `sev`, or `none`                         | `auto`                 |
| `RAM`                  | VM memory                                               | `16384M`               |
| `CPU`                  | vCPU count                                              | `4`                    |
| `DISK_SIZE`            | Disk image size                                         | `40G`                  |

## Run Trustee on the host

`qemu.sh` installs the Propeller stack _inside_ the CVM (Attestation Agent, CoCo Keyprovider, Proplet). Trustee is **not** part of that guest stack: it is the relying party that verifies the CVM's attestation evidence and releases the image decryption key, so it runs **outside the CVM**.

Where "outside" is depends on how the CVM is provisioned:

| CVM deployment             | Run KBS on                          | Guest reaches it at       |
| -------------------------- | ----------------------------------- | ------------------------- |
| Local QEMU CVM (`qemu.sh`) | The host running `qemu.sh`          | `http://10.0.2.2:8082`    |
| Cloud CVM (Azure/AWS/GCP)  | A separate VM or server you control | `https://<kbs-host>:8082` |

Never run KBS inside the CVM. If it ran inside the guest, the workload would hold the keys it is only supposed to receive _after_ attestation, and the trust boundary would collapse. For a cloud CVM the same rule applies more strictly: the CVM's threat model distrusts the hypervisor and host operator, so KBS must be a separate trust domain, not co-located with the machine hosting the CVM.

In the local QEMU setup the guest reaches the host through QEMU user-mode networking, where `10.0.2.2` is the NAT alias for the host's loopback. That is why `KBS_URL` defaults to `http://10.0.2.2:8082`: Trustee listens on the host and the guest dials `10.0.2.2:8082` to reach it.

### Start the stack

```bash
git clone https://github.com/confidential-containers/trustee
cd trustee

openssl genpkey -algorithm ed25519 > kbs/config/private.key
openssl pkey -in kbs/config/private.key -pubout -out kbs/config/public.pub

# Publish KBS on host port 8082 (compose defaults to 8080):
#   docker-compose.yml:  ports: ["8082:8080"]
docker compose up -d
```

For a first loopback-only test, leave KBS on plain HTTP (`insecure_http = true` in `kbs/config/docker-compose/kbs-config.toml`). The guest then uses the default `KBS_URL=http://10.0.2.2:8082`.

### Create and upload the image key

```bash
openssl rand -base64 32 | tr -d '\n' > private_key

cargo build --release   # builds kbs-client

./target/release/kbs-client \
  --url http://127.0.0.1:8082 \
  config set-resource \
  --resource-file private_key \
  --path default/key/propeller-addition
```

### Allow key release for a first run

```bash
./target/release/kbs-client \
  --url http://127.0.0.1:8082 \
  config set-resource-policy \
  --policy-file kbs/sample_policies/allow_all.rego
```

Tighten this once the end-to-end flow works. The path `default/key/propeller-addition` is the `kbs_resource_path` used in encrypted task definitions.

### Point KBS at the Attestation Service token CA

Trustee's `setup.sh` creates two separate PKI hierarchies in the same directory. KBS must verify the AS-issued token against the **token** CA, not the TLS CA, or key release fails with:

```text
TokenVerifierError: Cannot verify token: neither trusted jwk set nor trusted pem public key works
Failed to get KEK from KBS ... request unauthorized
```

```bash
cd ~/trustee/kbs/config/docker-compose

# extract the second cert (the token-signing root) from the AS chain
awk '/BEGIN CERTIFICATE/,/END CERTIFICATE/' token-cert-chain.pem \
  | awk 'BEGIN{n=0} /BEGIN CERTIFICATE/{n++} n==2' > token-ca-cert.pem
openssl verify -CAfile token-ca-cert.pem token-cert.pem   # expect: OK

cd ~/trustee
sed -i 's|trusted_certs_paths = \[.*\]|trusted_certs_paths = ["/opt/confidential-containers/kbs/user-keys/token-ca-cert.pem"]|' \
  kbs/config/docker-compose/kbs-config.toml
docker compose up -d --force-recreate kbs
```

### On TLS

KBS is used over plain HTTP, matching `KBS_URL=http://10.0.2.2:8082`. This is not laziness: `coco_keyprovider` links `reqwest` with `rustls-platform-verifier`, whose Linux path loads roots through `rustls-native-certs`. Pointing it at a private CA via `SSL_CERT_FILE` did not work in testing — the handshake failed with `UnknownIssuer` while `openssl s_client` with the same CA succeeded — so a self-signed KBS certificate cannot be trusted by the keyprovider.

The hop never leaves the host, and the image key is additionally wrapped under an RCAR-derived session key, so HTTP is an acceptable trade here. If you need TLS, use a certificate from a publicly trusted CA against a real DNS name (for the local QEMU case the guest addresses the host as `10.0.2.2`, which no public CA will issue for), or route KBS through a component that reads the system trust store reliably. For the cloud-CVM equivalent, see [`../azure-sev/README.md`](../azure-sev/README.md).

### Verify from the guest

```bash
ssh -p 2222 propeller@localhost
curl -k https://10.0.2.2:8082/kbs/v0/resource-policy
```

A JSON error (rather than a connection refused, timeout, or certificate error) means the guest reached KBS. Trustee only needs to be up before an encrypted workload runs; the Attestation Agent retries.

For the equivalent setup on a cloud CVM (Trustee on a separate VM, guest stack inside the CVM), see:

| Cloud CVM                 | Guide                                                                                                                                    |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Azure AMD SEV-SNP         | [`../azure-sev/README.md`](../azure-sev/README.md) or [propeller.absmach.eu/docs/azure-cvm](https://propeller.absmach.eu/docs/azure-cvm) |
| GCP Intel TDX (C3/C4)     | [`../gcp-tdx/README.md`](../gcp-tdx/README.md)                                                                                           |
| GCP AMD SEV-SNP (N2D/C4D) | [`../gcp-sev/README.md`](../gcp-sev/README.md)                                                                                           |

## Run

The script re-executes itself with `sudo -E` to preserve exported variables.

```bash
export PROPLET_TENANT_ID="your-tenant-id"
export PROPLET_ENTITY_ID="your-entity-id"
export PROPLET_API_KEY="your-api-key"
export PROPLET_CHANNEL_ID="your-channel-id"
export PROPLET_MQTT_ADDRESS="tcp://mqtt.example.com:1883"
export KBS_URL="http://10.0.2.2:8082"

# Build and run (default)
./qemu.sh

# Build only
./qemu.sh build

# Run an existing image
./qemu.sh run
```

CVM mode is auto-detected. Override with `ENABLE_CVM=tdx`, `ENABLE_CVM=sev`, or `ENABLE_CVM=none`.

First boot takes 10–15 minutes while cloud-init compiles Wasmtime, Attestation Agent, CoCo Keyprovider, and Proplet from source. Subsequent boots start all services immediately.

## Access

```bash
ssh -p 2222 propeller@localhost
# password: propeller
```

| Host port | Service           |
| --------- | ----------------- |
| `2222`    | SSH               |
| `50010`   | Attestation Agent |
| `50011`   | CoCo Keyprovider  |

```bash
sudo systemctl status attestation-agent coco-keyprovider proplet
sudo journalctl -u proplet -f
```
