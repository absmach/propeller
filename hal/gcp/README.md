# Propeller on an Intel TDX GCP Confidential VM with Trustee

Deploy [Trustee](https://github.com/confidential-containers/trustee) **outside** the confidential VM and run the Propeller stack with the CoCo **guest
components** **inside** a GCP Intel TDX confidential VM. This is the production-shaped topology: the relying party (Trustee) verifies evidence and releases keys, and it must not share the trust boundary with the workload it is attesting.

This is the Intel TDX counterpart of [`../azure/README.md`](../azure/README.md), which covers AMD SEV-SNP on Azure. Everything in Parts 4–6 (publishing an encrypted image, running a workload, exercising the HAL) is identical between the two; Parts 1–3 are where the platforms differ, and those differences are called out as they come up.

## Architecture

| Component                               | Runs on                | Role                                                         |
| --------------------------------------- | ---------------------- | ------------------------------------------------------------ |
| KBS (Key Broker Service)                | Trustee host (outside) | Validates the attestation token, releases decryption keys    |
| AS (Attestation Service)                | Trustee host (outside) | Verifies TEE evidence (here: `tdx`)                          |
| RVPS (Reference Value Provider Service) | Trustee host (outside) | Reference values for verification                            |
| Attestation Agent (AA)                  | CVM (inside)           | Collects a TDX quote from the Linux TSM, performs the RCAR handshake |
| CoCo Keyprovider                        | CVM (inside)           | Image key-wrap/unwrap keyprovider for `image-rs`             |
| Proplet                                 | CVM (inside)           | Pulls and runs the encrypted workload                        |
| Wasmtime                                | CVM (inside)           | Executes the decrypted WASM                                  |

### Where to run Trustee

Trustee runs on a **separate GCE VM you control** — the _Trustee host_ — never inside the CVM and never on the Google host. The whole point of a CVM is that the machine hosting it (and its operator) is untrusted, so the component that verifies evidence and releases decryption keys must be its own trust domain. Put the Trustee host in the same VPC network as the CVM and have the CVM reach KBS over the private network; do not expose KBS publicly.

| Placement                                                | Correct?                                   |
| -------------------------------------------------------- | ------------------------------------------ |
| A separate GCE VM in the same VPC (internal IP)           | Yes                                        |
| Any machine or server you control, reachable from the CVM | Yes                                        |
| Inside the CVM                                            | No — the keys would live with the workload |
| The CVM's Google host / hypervisor                        | No — you do not control it                 |

Provisioning is in [Part 2](#part-2--deploy-trustee-outside-the-cvm).

## What is different from the Azure AMD SEV-SNP guide

Intel TDX on GCP differs from AMD SEV-SNP on Azure in ways that matter while building the guest stack:

|                          | Azure AMD SEV-SNP CVM                                | GCP Intel TDX CVM                                                     |
| ------------------------ | ---------------------------------------------------- | --------------------------------------------------------------------- |
| Attester                 | `az-snp-vtpm-attester`                               | `tdx-attester`                                                        |
| Evidence source          | Guest vTPM (HCL report + TPM quote)                  | Intel DCAP quote from the Linux TSM (`/dev/tdx_guest`)                |
| Enable the platform      | `--security-type ConfidentialVM --enable-vtpm true`  | `--confidential-compute-type=TDX`                                     |
| Machine family           | `DCasv5` / `ECasv5`                                  | `c3-standard-*` (`c4-standard-*` in preview)                          |
| Image                    | `Canonical:…server-cvm` (a CVM-specific image)        | Any `TDX_CAPABLE` image, e.g. `ubuntu-os-cloud` `ubuntu-2404-lts-amd64` |
| Secure Boot / vTPM flags | Required                                              | Not required; `--shielded-secure-boot` is optional                    |
| DCAP dev headers         | Needed for the keyprovider build                      | Not needed inside the guest — `tdx-attester` talks to the TSM directly |
| Boot chain measured into | PCRs via the vTPM                                     | RTMRs via the TDX quote; GCP boots GRUB, not the TDVF shim            |
| Guest verification       | `Detected confidential virtualization sev-snp`        | `Memory Encryption Features active: TDX`                             |

Two of these are easy to get wrong:

- **Do not carry the Azure flags over.** `--enable-vtpm` and the CVM image family do not exist here, and TDX evidence does not come from a vTPM. A TDX CVM with no `/dev/tdx_guest` cannot produce evidence at all.
- **The guest does not need Intel's DCAP headers.** The Azure guest build fails without `libsgx-dcap-quote-verify-dev` because `az-snp-vtpm` links `tss-esapi`. `tdx-attester` uses the kernel TSM ioctls instead, so the guest build has no such dependency. The KBS client build on the Trustee host still needs the DCAP headers — see [2.5](#25-create-and-upload-the-image-key).

## Prerequisites

- **CVM**: a GCP project with quota for the `c3` machine family in a TDX-supported zone, `gcloud` configured, and an SSH key. TDX availability is per-zone and changes; see [1.1](#11-pick-a-zone-that-has-tdx).
- **Trustee host**: a plain GCE VM (no confidential SKU required), provisioned in [Part 2.1](#21-provision-the-trustee-host) in the same VPC as the CVM. Docker and Docker Compose installed.
- **Client tools** (on the Trustee host): `cargo` (for `kbs-client`), `skopeo`, `wasm-to-oci`.
- The CVM must reach the Trustee host on the KBS port (default `8082`) over the private network. A default VPC network already permits this; [Part 2.1](#21-provision-the-trustee-host) shows the explicit firewall rule if you want one.

Both VMs are created from scratch below, in one project.

## Part 1 — Provision the Intel TDX CVM

Set the project and zone once and keep the same shell for the rest of the guide:

```bash
gcloud auth login
gcloud config set project <project-id>
PROJECT=<project-id>
ZONE=us-central1-a

gcloud compute instances create propeller-intel-tdx-cvm \
  --project "$PROJECT" \
  --zone "$ZONE" \
  --machine-type=c3-standard-4 \
  --confidential-compute-type=TDX \
  --maintenance-policy=TERMINATE \
  --image-family=ubuntu-2404-lts-amd64 \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=40G \
  --tags=propeller \
  --metadata=enable-oslogin=false
```

These flags are what make the CVM work:

| Flag                                | Why it is required                                                                                                                        |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `--machine-type=c3-standard-*`      | Intel TDX is only offered on the C3 (Sapphire Rapids) and C4 (Granite Rapids, preview) families. A non-C3/C4 machine type fails at creation. |
| `--confidential-compute-type=TDX`   | This is what puts the VM in a TD. Without it the same machine type produces a regular VM with no evidence and no `/dev/tdx_guest`.       |
| `--maintenance-policy=TERMINATE`    | Live migration is not supported for TDX instances; `gcloud` rejects the create without it.                                                  |
| `--image-family` / `--image-project` | Must be an image tagged `TDX_CAPABLE`. The stock `ubuntu-os-cloud` families are; a random image is not, and its kernel may lack the TDX guest driver. |

`--metadata=enable-oslogin=false` keeps OS Login out of the way so you can SSH with the key below. Drop it if you already use OS Login.

### 1.1 Pick a zone that has TDX

TDX is offered in a subset of zones, and the list moves over time — the authoritative table is [Supported configurations](https://cloud.google.com/confidential-computing/confidential-vm/docs/supported-configurations). `us-central1-a` supports `c3-standard-*`; plenty of regions do not.

To check from the CLI that a candidate zone offers the machine type at all:

```bash
gcloud compute machine-types list \
  --filter="name:c3-standard-4 AND zone:~$ZONE" \
  --format="value(name)"
```

Being offered in the zone is necessary but not sufficient — a plain (non-TDX) `c3-standard-4` is the same machine type, and the zone table above is the only authoritative answer. If the create in Part 1 fails with a confidential-compute error, that is the reason.

Confirm the image supports TDX isolation:

```bash
gcloud compute images describe ubuntu-2404-lts-amd64 \
  --project ubuntu-os-cloud \
  --format="value(guestOsFeatures)"
# expect: type: TDX_CAPABLE  (among others)
```

### 1.2 Connect and record the addresses

A stock GCP image has no `propeller` user, so create one and install your key — the rest of the guide assumes you SSH as `propeller`:

```bash
CVM_IP=$(gcloud compute instances describe propeller-intel-tdx-cvm \
  --zone "$ZONE" --format='get(networkInterfaces[0].accessConfigs[0].natIP)')

sudo adduser --disabled-password --gecos "" propeller
sudo adduser propeller sudo
sudo install -d -m 700 -o propeller -g propeller /home/propeller/.ssh
sudo tee -a /home/propeller/.ssh/authorized_keys < ~/.ssh/cloud.pub
sudo chown propeller:propeller /home/propeller/.ssh/authorized_keys

ssh -i ~/.ssh/cloud propeller@$CVM_IP
```

`gcloud compute ssh propeller-intel-tdx-cvm --zone "$ZONE" -I ~/.ssh/cloud.pub` is the shortcut: it creates the user and installs the key itself, but under your Google account name — use that name in place of `propeller` for the rest of the guide if you take this route.

Use the instance's **external** IP for SSH, and its **internal** IP for everything the guest dials (KBS in Part 3). Keeping those two separate is the whole point of the split in the architecture table above.

### 1.3 Confirm the guest is really in a TD

Do this before building anything. A plain C3 VM and a TDX CVM are the same machine type, so nothing outside the guest tells you which one you got — the creation flag does, and this is where you confirm it:

```bash
sudo dmesg | grep -i -e tdx -e "Memory Encryption"
# expect: Memory Encryption Features active: TDX

ls -l /dev/tdx_guest
# expect: /dev/tdx_guest present

ls -d /sys/kernel/config/tsm /sys/kernel/config/tsm/report 2>/dev/null
# the TSM configfs interface the attester and the HAL use to request a quote
grep -o 'tdx_guest' /proc/cpuinfo | head -1
```

If `Memory Encryption Features active: TDX` is missing, the VM is not confidential: delete it and re-create with `--confidential-compute-type=TDX`.

If `/dev/tdx_guest` is missing while TDX is active, the guest kernel lacks the driver module:

```bash
sudo modprobe tdx_guest
ls -l /dev/tdx_guest
```

If the module does not exist, the image is not `TDX_CAPABLE` — use a different image family.

Note what is *absent*: unlike the Azure CVM there is no `/dev/tpm0`, and `/dev/sev*` does not exist either. Both are correct. On TDX the evidence is a hardware quote, not a vTPM attestation, so no vTPM is provisioned and none is needed.

## Part 2 — Deploy Trustee outside the CVM

All of this runs on `TRUSTEE_HOST`, never inside the CVM.

### 2.1 Provision the Trustee host

Create a plain GCE VM in the same VPC as the CVM. It does not need a confidential SKU — it is the relying party, not the workload.

```bash
gcloud compute instances create propeller-trustee-host \
  --project "$PROJECT" \
  --zone "$ZONE" \
  --machine-type=e2-standard-2 \
  --image-family=ubuntu-2404-lts-amd64 \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=40G \
  --tags=propeller \
  --metadata=enable-oslogin=false
```

Unlike a CVM this one needs no confidential flags. Record both addresses — the guest dials the **internal** one, you SSH to the **external** one:

```bash
TRUSTEE_INTERNAL=$(gcloud compute instances describe propeller-trustee-host \
  --zone "$ZONE" --format='get(networkInterfaces[0].networkIP)')
TRUSTEE_EXTERNAL=$(gcloud compute instances describe propeller-trustee-host \
  --zone "$ZONE" --format='get(networkInterfaces[0].accessConfigs[0].natIP)')

echo "TRUSTEE_HOST=$TRUSTEE_INTERNAL   # what the guest dials"
echo "TRUSTEE_SSH=$TRUSTEE_EXTERNAL"
```

In a default VPC network the CVM can already reach `8082`: the built-in `default-allow-internal` rule permits all TCP from `10.128.0.0/9`, and neither instance has an external-IP-restricted egress. Nothing more is needed for the flow below.

Add an explicit rule anyway if you want KBS reachable by intent rather than by accident — for instance because you deleted `default-allow-internal`, use a hardened network, or want the permission visible in review:

```bash
# Both instances carry the network tag `propeller`, so this keeps working even
# if an ephemeral IP is reassigned. Never use --source-ranges=0.0.0.0/0 here:
# KBS must not be reachable from the internet.
gcloud compute firewall-rules create allow-kbs-from-propeller \
  --project "$PROJECT" \
  --allow=tcp:8082 \
  --source-tags=propeller \
  --target-tags=propeller \
  --network=default \
  --direction=INGRESS \
  --priority=1000
```

Using a single tag for both roles means the rule also permits the CVM to reach the Trustee host on `22`, which `default-allow-ssh` already allows. To keep the roles separate, tag the Trustee host `propeller-kbs` and use `--target-tags=propeller-kbs` with `--source-tags=propeller`.

Export the addresses for the rest of the guide (still in the same shell):

```bash
export TRUSTEE_HOST=$TRUSTEE_INTERNAL
export TRUSTEE_SSH=$TRUSTEE_EXTERNAL
export CVM_HOST=$CVM_IP
```

Install Docker and Docker Compose on the Trustee host (the next steps run there):

```bash
ssh -i ~/.ssh/cloud propeller@$TRUSTEE_SSH
sudo apt update
sudo apt upgrade -y
sudo apt install -y git curl
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"   # log out and back in to take effect
sudo systemctl enable docker.service
```

The Trustee host needs a `propeller` user too — create it the same way as in [1.2](#12-connect-and-record-the-addresses) before the SSH above.

### 2.2 Start the Trustee stack

KBS runs over **plain HTTP inside the VPC**. This is deliberate and is what the rest of this guide assumes.

The reason is a trust-store problem on the guest: `coco_keyprovider` links `reqwest` with `rustls-platform-verifier`, which on Linux loads roots through `rustls-native-certs`. Pointing it at a private CA via `SSL_CERT_FILE` did not work in testing — the handshake still failed with `UnknownIssuer` while `openssl s_client` against the same CA succeeded — so a self-signed KBS certificate cannot be trusted by the keyprovider. Since TLS here only protects a hop that never leaves the private network, and the image key is additionally wrapped under an RCAR-derived session key, HTTP is an acceptable trade for a working deployment. If you need TLS, terminate it at a component that reads the system store reliably, or issue the KBS certificate from a publicly trusted CA against a real DNS name.

Generate the KBS signing keys and start the stack:

```bash
git clone https://github.com/confidential-containers/trustee
cd trustee

openssl genpkey -algorithm ed25519 > kbs/config/private.key
openssl pkey -in kbs/config/private.key -pubout -out kbs/config/public.pub
```

KBS listens on `8080` inside the container by default and compose publishes `8080:8080`. This guide publishes KBS on `8082` so it does not collide with anything else you run on the host. Check the `kbs` service mapping:

```bash
awk '/^  kbs:/,/^  [a-z]/' docker-compose.yml | grep -A3 ports
```

If it publishes `8080:8080`, change the host side to `8082:8080`.

Ensure `[http_server]` in `kbs/config/docker-compose/kbs-config.toml` reads:

```toml
[http_server]
sockets = ["0.0.0.0:8080"]
insecure_http = true
```

`sockets` is the in-container bind address and stays `0.0.0.0:8080` — the host port comes from the compose mapping. `insecure_http = true` is already the shipped default.

Start the stack:

```bash
docker compose up -d
docker compose ps
```

### 2.3 Check that KBS trusts the Attestation Service's token CA

Trustee's `setup.sh` generates two independent PKI hierarchies in the same directory:

| File                                                  | Purpose                                                       |
| ----------------------------------------------------- | ------------------------------------------------------------- |
| `ca-cert.pem`, `ca.key`                               | **token-signing** CA (`CN=KBS-compose-root`) used by the AS    |
| `token.key`, `token-cert.pem`, `token-cert-chain.pem` | AS EAR-token signing key/cert                                 |
| `tls-*.pem`                                           | TLS endpoints, unrelated to attestation                       |

KBS verifies the AS-issued token against `[attestation_token].trusted_certs_paths`, and it must point at the **token** CA, not the TLS CA. Current Trustee already does — the shipped value is `ca-cert.pem`, which is exactly the token-signing root. Confirm that rather than assuming it:

```bash
cd ~/trustee/kbs/config/docker-compose
grep -A2 '\[attestation_token\]' kbs-config.toml
# expect: trusted_certs_paths = ["/opt/confidential-containers/kbs/user-keys/ca-cert.pem"]

# the second certificate in the chain is the token-signing root
awk '/BEGIN CERTIFICATE/,/END CERTIFICATE/' token-cert-chain.pem \
  | awk 'BEGIN{n=0} /BEGIN CERTIFICATE/{n++} n==2' > token-ca-cert.pem

# sanity: the AS token cert must verify against it
openssl verify -CAfile token-ca-cert.pem token-cert.pem
# expect: token-cert.pem: OK
```

If the verify passes and `trusted_certs_paths` already names `ca-cert.pem`, you are done — leave the config alone. If it names a `tls-*` certificate, or the verify fails, point KBS at the extracted CA and restart it:

```bash
cd ~/trustee
sed -i 's|trusted_certs_paths = \[.*\]|trusted_certs_paths = ["/opt/confidential-containers/kbs/user-keys/token-ca-cert.pem"]|' \
  kbs/config/docker-compose/kbs-config.toml
docker compose up -d --force-recreate kbs
```

Getting this wrong produces a confusing failure in the guest, not here:

```text
TokenVerifierError: Cannot verify token: neither trusted jwk set nor trusted pem public key works
Failed to get KEK from KBS ... request unauthorized
```

### 2.4 Verify KBS is reachable

Always use the **private IP**, never `0.0.0.0` — that is a bind address, not a connect address:

```bash
curl -s -o /dev/null -w 'http=%{http_code}\n' http://${TRUSTEE_HOST}:8082/kbs/v0/resource-policy
# expect http=401 — a JSON auth error, not a connection error
```

### 2.5 Create and upload the image key

`kbs-client` is built from the Trustee tree. On Ubuntu the build pulls in the Intel SGX DCAP verifier via `intel-tee-quote-verification-sys`, whose build script needs the **DCAP dev headers** — this is true on a TDX deployment too, because the verifier is part of KBS rather than of the guest attester. Without the headers the build fails with:

```text
error: failed to run custom build command for `intel-tee-quote-verification-sys v0.3.0`
bindings.h:32:10: fatal error: 'sgx_dcap_quoteverify.h' file not found
```

Install the toolchain and the SGX DCAP headers:

```bash
sudo apt-get update
sudo apt-get install -y \
  build-essential gcc make pkg-config libssl-dev openssl curl git \
  clang cmake jq unzip libtss2-dev tpm2-tools \
  libclang-dev libsgx-dcap-quote-verify-dev libsgx-dcap-default-qpl \
  protobuf-compiler

curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
. "$HOME/.cargo/env"
```

The headers come from Intel's `libsgx-dcap-quote-verify-dev` package. If `apt-get` cannot find it, add Intel's repository:

```bash
curl -fsSL https://download.01.org/intel-sgx/sgx_repo/ubuntu/intel-sgx-deb.key \
  | sudo gpg --dearmor -o /usr/share/keyrings/intel-sgx.gpg
echo "deb [arch=amd64 signed-by=/usr/share/keyrings/intel-sgx.gpg] https://download.01.org/intel-sgx/sgx_repo/ubuntu $(lsb_release -cs) main" \
  | sudo tee /etc/apt/sources.list.d/intel-sgx.list
sudo apt-get update
sudo apt-get install -y libsgx-dcap-quote-verify-dev
```

Confirm the header is present before building:

```bash
find /usr/include -name 'sgx_dcap_quoteverify.h'
```

Then generate the key and build the client:

```bash
openssl rand -base64 32 | tr -d '\n' > private_key

cargo build --release

# Upload the key under the resource path used by tasks
./target/release/kbs-client \
  --url http://${TRUSTEE_HOST}:8082 \
  config set-resource \
  --resource-file private_key \
  --path default/key/propeller-addition
```

You are the **admin** here, so this only needs the KBS address — the default `admin` authorization in `kbs-config.toml` accepts it. Over HTTP there is no `--cert-file`.

### 2.6 Set the resource policy

For a first end-to-end run, allow all so policy is not what blocks you:

```bash
./target/release/kbs-client \
  --url http://${TRUSTEE_HOST}:8082 \
  config set-resource-policy \
  --policy-file kbs/sample_policies/allow_all.rego
```

Once the flow works, replace this with a policy that requires a real TDX token — see [Tightening the attestation policy](#tightening-the-attestation-policy).

## Part 3 — Guest stack inside the CVM

Everything in this part runs inside the confidential VM:

```bash
ssh -i ~/.ssh/cloud propeller@$CVM_HOST   # admin user is propeller (Part 1)
                             # if you brought an existing CVM
```

### 3.1 Install build dependencies and runtimes

```bash
sudo apt install -y git curl
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"   # log out and back in to take effect
sudo systemctl enable docker.service

sudo apt-get update
sudo apt-get install -y \
  build-essential gcc make pkg-config libssl-dev openssl curl git \
  clang cmake jq unzip netcat-openbsd

curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
. "$HOME/.cargo/env"

WASMTIME_VERSION=v48.0.2
curl -L "https://github.com/bytecodealliance/wasmtime/releases/download/${WASMTIME_VERSION}/wasmtime-${WASMTIME_VERSION}-x86_64-linux.tar.xz" -o /tmp/wasmtime.tar.xz
tar -xf /tmp/wasmtime.tar.xz -C /tmp
sudo mv /tmp/wasmtime-${WASMTIME_VERSION}-x86_64-linux/wasmtime /usr/local/bin/
rm -rf /tmp/wasmtime*
wasmtime --version
```

Note what is **not** in that list, compared to the Azure guest: no `libtss2-dev`, no `tpm2-tools`, no `libsgx-dcap-*`. The TDX attester talks to `/dev/tdx_guest` and `/sys/kernel/config/tsm`, so the guest needs no TPM or DCAP userspace at all.

### 3.2 Build Attestation Agent and CoCo Keyprovider with the TDX attester

Build with the **`tdx-attester`**, and only that one:

```bash
cd /tmp
git clone https://github.com/rodneyosodo/guest-components.git
cd guest-components
git checkout enable-wasm-workloads

# Attestation Agent (gRPC) with the Intel TDX attester only
cd attestation-agent
make ATTESTER=tdx-attester ttrpc=false
sudo make install

# CoCo Keyprovider — same attester; do NOT use the default (all-attesters)
cd coco_keyprovider
cargo build --release --target x86_64-unknown-linux-gnu \
  --no-default-features --features tdx-attester
sudo cp ../../target/x86_64-unknown-linux-gnu/release/coco_keyprovider /usr/local/bin/

attestation-agent --help >/dev/null && echo "AA OK"
coco_keyprovider --help >/dev/null && echo "keyprovider OK"
```

Two things are easy to get wrong here:

- **Do not use `all-attesters`.** That enables the NVIDIA attester, whose build script needs the NVIDIA C++ attestation SDK and fails with `Header file not found at ".../nv-attestation-sdk-cpp/build/include/nvat.h"`. Passing `ATTESTER=tdx-attester` to `make` replaces `all-attesters` rather than adding to it.
- **`tdx-attester` must not be combined with `tpm-attester`.** With a generic TPM attester compiled in, `detect_attestable_devices()` adds a TPM as an _additional_ device and composite evidence then demands a quote for a TPM that is not bound to the TD. Do not add it.

If the keyprovider's `Cargo.toml` does not expose a `tdx-attester` feature, add it (and make `kbs_protocol` non-default) so the attester set is selectable at build time:

```toml
kbs_protocol = { path = "../kbs_protocol", default-features = false, features = [
    "background_check",
    "rust-crypto",
] }

[features]
default = ["all-attesters"]
all-attesters = ["kbs_protocol/all-attesters"]
tdx-attester = ["kbs_protocol/tdx-attester"]
```

### 3.3 Build Proplet

```bash
git clone https://github.com/absmach/propeller.git ~/propeller
cd ~/propeller/proplet
cargo build --release
sudo cp target/release/proplet /usr/local/bin/
```

No feature flags: one proplet binary serves both platforms. It is compiled with the `tdx-attester` and the `az-snp-vtpm-attester` together, and with both HAL platforms, and picks at runtime — `detect_tee_type()` probes TDX first and falls through to the Azure vTPM, so the running platform decides which one is primary.

### 3.4 Write the Attestation Agent config

The AA config must have a `.toml` (or `.json`) extension. The loader picks its parser from the filename, and anything else fails at startup with:

```text
Failed to load attestation agent config: configuration file
"/etc/attestation-agent.conf" is not of a supported file format
```

Over HTTP there is no certificate to embed, so the config is just the KBS URL:

```bash
sudo tee /etc/attestation-agent.toml >/dev/null <<EOF
[token_configs]

[token_configs.kbs]
url = "http://${TRUSTEE_HOST}:8082"

[eventlog_config]
init_pcr = 17
enable_eventlog = false

[log]
level = "info"
EOF
```

`init_pcr` is only meaningful for a TPM-backed eventlog. On TDX the runtime measurements are the RTMRs in the quote and `enable_eventlog = false` keeps the AA from trying to extend a PCR that has no meaning here — leave it off.

Point proplet at it (`PROPLET_AA_CONFIG_PATH=/etc/attestation-agent.toml`) in [3.5](#35-run-the-services).

Verify the guest can reach KBS before starting services:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://${TRUSTEE_HOST}:8082/kbs/v0/resource-policy
# expect 401 — a JSON auth error, not a connection error
```

### 3.5 Run the services

Create `/etc/default/proplet` (used by the unit below):

```bash
sudo tee /etc/default/proplet >/dev/null <<EOF
PROPLET_LOG_LEVEL=info
PROPLET_INSTANCE_ID=$(cat /proc/sys/kernel/random/uuid)
PROPLET_TENANT_ID=${PROPLET_TENANT_ID}
PROPLET_ENTITY_ID=${PROPLET_ENTITY_ID}
PROPLET_API_KEY=${PROPLET_API_KEY}
PROPLET_CHANNEL_ID=${PROPLET_CHANNEL_ID}
PROPLET_MQTT_ADDRESS=${PROPLET_MQTT_ADDRESS}
PROPLET_MQTT_TIMEOUT=30
PROPLET_MQTT_QOS=2
PROPLET_EXTERNAL_WASM_RUNTIME=/usr/local/bin/wasmtime
PROPLET_HAL_ENABLED=true
PROPLET_KBS_URI=http://${TRUSTEE_HOST}:8082
PROPLET_AA_CONFIG_PATH=/etc/attestation-agent.toml
PROPLET_LAYER_STORE_PATH=/tmp/proplet/layers
EOF
```

The three unit files are the same shape as the ones cloud-init writes in [`hal/ubuntu/qemu.sh`](../ubuntu/qemu.sh): AA on `127.0.0.1:50010`, CoCo Keyprovider on `127.0.0.1:50011`, then Proplet. Install them:

```bash
sudo mkdir -p /run/attestation-agent /run/coco-keyprovider /var/cache/wasmtime

sudo tee /etc/systemd/system/attestation-agent.service >/dev/null <<'EOF'
[Unit]
Description=Attestation Agent for Confidential Containers
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStartPre=/bin/mkdir -p /run/attestation-agent
ExecStart=/usr/local/bin/attestation-agent --attestation_sock 127.0.0.1:50010
Restart=on-failure
RestartSec=5s
Environment=RUST_LOG=info

[Install]
WantedBy=multi-user.target
EOF

sudo tee /etc/systemd/system/coco-keyprovider.service >/dev/null <<'EOF'
[Unit]
Description=CoCo Keyprovider for Confidential Containers
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStartPre=/bin/mkdir -p /run/coco-keyprovider
ExecStart=/usr/local/bin/coco_keyprovider --socket 127.0.0.1:50011 --kbs http://<TRUSTEE_HOST>:8082
Restart=on-failure
RestartSec=5s
Environment=RUST_LOG=info

[Install]
WantedBy=multi-user.target
EOF

sudo tee /etc/systemd/system/proplet.service >/dev/null <<'EOF'
[Unit]
Description=Proplet WebAssembly Workload Orchestrator
After=network-online.target attestation-agent.service coco-keyprovider.service
Wants=network-online.target
Requires=attestation-agent.service coco-keyprovider.service

[Service]
Type=simple
EnvironmentFile=/etc/default/proplet
Environment=WASMTIME_HOME=/var/lib/proplet
Environment=WASMTIME_CACHE_DIR=/var/cache/wasmtime
ExecStartPre=/bin/mkdir -p /var/lib/proplet/cache /var/cache/wasmtime
ExecStartPre=/bin/sh -c 'until nc -z 127.0.0.1 50010 && nc -z 127.0.0.1 50011; do sleep 1; done'
ExecStart=/usr/local/bin/proplet
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now attestation-agent coco-keyprovider proplet
sudo systemctl status attestation-agent coco-keyprovider proplet --no-pager
```

Substitute `<TRUSTEE_HOST>` in the `coco-keyprovider` unit with the Trustee host's private IP before enabling the services.

### 3.6 Watch the services

```bash
sudo journalctl -u attestation-agent -f
sudo journalctl -u coco-keyprovider -f
sudo journalctl -u proplet -f
```

A successful proplet start logs `TEE runtime initialized successfully`. When a task runs, the keyprovider logs an RCAR handshake and the AS logs an endorsement check for the TDX verifier.

Note `proplet.service` has `Requires=coco-keyprovider.service`, so stopping the keyprovider also stops proplet — restart both together.

## Part 4 — Encrypt and publish a WASM image

These commands run on the **Trustee host** (or any machine with the tools and network access to the registry).

```bash
wget https://github.com/tinygo-org/tinygo/releases/download/v0.42.0/tinygo_0.42.0_amd64.deb
sudo dpkg -i tinygo_0.42.0_amd64.deb
rm -f tinygo_0.42.0_amd64.deb

wget https://github.com/engineerd/wasm-to-oci/releases/download/v0.1.2/linux-amd64-wasm-to-oci
sudo mv linux-amd64-wasm-to-oci /usr/local/bin/wasm-to-oci
sudo chmod +x /usr/local/bin/wasm-to-oci

# Build a sample WASM (from the Propeller repo)
cd ~/propeller
GOOS=js GOARCH=wasm tinygo build -buildmode=c-shared -o build/addition.wasm -target wasi examples/addition/addition.go

# Log in to the Docker registry
docker login docker.io

# Push the plaintext image
wasm-to-oci push build/addition.wasm docker.io/<you>/tee-wasm-addition:latest --server docker.io

# Encrypt it with the key stored in KBS
mkdir -p output
docker run \
  -v "$PWD/output:/output" \
  docker.io/rodneydav/coco-keyprovider:latest \
  /encrypt.sh \
  -k "$(cat ./private_key)" \
  -i kbs:///default/key/propeller-addition \
  -s docker://docker.io/<you>/tee-wasm-addition:latest \
  -d dir:/output

# Install skopeo
sudo apt install -y skopeo

# Push the encrypted image
skopeo login docker.io
skopeo copy dir:$(pwd)/output docker://<you>/tee-wasm-addition:encrypted
```

`skopeo copy` pushes directly to the registry — **do not** follow it with `docker push docker://...`. `docker push` does not accept the `docker://` scheme and fails with `invalid reference format`.

`Copying blob ... skipped: already exists` during the copy is normal — the plaintext layer is already in the registry from the `wasm-to-oci push`, and only the encrypted layer needs uploading. If `skopeo copy` prints `Writing manifest to image destination`, the push succeeded.

## Part 5 — Run an encrypted workload and verify

Submit a task. Two fields are easy to get wrong:

- **`image_url` must be a bare OCI reference** — no `docker://` scheme. That prefix belongs to `skopeo`/`wasm-to-oci` commands, not to task definitions. Passing it produces `Failed to parse image reference: invalid reference format` before any attestation happens.
- **Do not include a `file` field** for encrypted workloads.

```json
{
  "name": "add",
  "image_url": "<you>/tee-wasm-addition:encrypted",
  "encrypted": true,
  "kbs_resource_path": "default/key/propeller-addition",
  "cli_args": ["--invoke", "add"],
  "inputs": [10, 20]
}
```

```bash
curl -s -X POST http://localhost:7070/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "add",
    "image_url": "<you>/tee-wasm-addition:encrypted",
    "encrypted": true,
    "kbs_resource_path": "default/key/propeller-addition",
    "cli_args": ["--invoke", "add"],
    "inputs": [10, 20]
  }'
```

If the task was created but shows `no active proplets available` when started, proplet is not connected — check `systemctl is-active proplet` first.

```bash
TID=<task id>
curl -s -X POST "http://localhost:7070/tasks/$TID/start"
sleep 15
curl -s "http://localhost:7070/tasks/$TID" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('state:',d['state']); print('results:',repr(d.get('results'))); print('error:',d.get('error'))"
```

`state: 3` with `results: '30\n'` is success — that is `10 + 20`, decrypted and executed inside the TD.

A successful run means the whole chain worked: the AA produced a TDX quote, the AS verified it, KBS released the key, CoCo Keyprovider unwrapped it, and `image-rs` decrypted the layer inside the CVM.

### Reading failures

| Error                               | Stage              | Meaning                                                                                       |
| ----------------------------------- | ------------------ | --------------------------------------------------------------------------------------------- |
| `Failed to parse image reference`   | task validation    | `docker://` in `image_url`                                                                    |
| `Failed to pull and decrypt layers` | image pull         | see the keyprovider log for the real cause                                                    |
| `Failed to get KEK from KBS`        | key release        | KBS refused; check the keyprovider log for `UnknownIssuer`, `PolicyDeny`, or `TokenVerifierError` |
| `TokenVerifierError`                | token verification | KBS is trusting the wrong CA — see [2.3](#23-check-that-kbs-trusts-the-attestation-services-token-ca) |
| `no attestable device` / `get device measurement failed` | attestation | the guest has no `/dev/tdx_guest`, or the AA was built with the wrong attester — see [1.3](#13-confirm-the-guest-is-really-in-a-td) and [3.2](#32-build-attestation-agent-and-coco-keyprovider-with-the-tdx-attester) |

The keyprovider log is where the real error lives; proplet's message is a summary:

```bash
sudo journalctl -u coco-keyprovider --since "2 min ago" --no-pager | tail -20
```

### Tightening the attestation policy

`allow_all` was used above so that policy is not what blocks the first run. It releases the key to any guest that can reach KBS, which defeats the point of attesting.

To require a genuine TDX guest, you need two things: reference values for the fields Trustee's `default_cpu` policy checks, and a resource policy that inspects the resulting attestation token. Two details are specific to this platform:

- **GCP boots GRUB, not the TDVF shim.** Trustee's TDX policy has a branch for the shim/TDVF boot flow, which checks the UEFI event log for `File(kernel)` and `LOADED_IMAGE::LoadOptions`. A stock GCP VM does not produce those events, so evaluation takes the other branch, which compares `rtmr_1` and `rtmr_2` against reference values. Expect `rtmr_1` to change on every kernel or initrd update, and treat its reference value as something you re-collect and re-upload when the guest image changes.
- **The claims come from the quote, not from a vTPM**, so the reference values are TDX-specific: `mr_td` (the TD firmware measurement), `rtmr_1`, `rtmr_2`, and `xfam` (the enabled TD attributes). There is no `measurement` claim to pin the way an Azure vTPM guest has one.

Collect the values from a quote on your own guest first — the AS log or a `trustauthority-cli evidence --tdx` run both expose them — then upload them through the RVPS and write a resource policy that requires the TDX submod's trust vector. Treat this as the follow-up work after the end-to-end flow works, not as part of getting there.

## Part 6 — Test the HAL interfaces directly

This is independent of Trustee and only exercises the WASM-facing HAL providers. It is useful to confirm the non-TEE interfaces work inside the CVM.

```bash
cd ~/propeller
rustup target add wasm32-wasip2
make hal-runner hal-test attestation-test

./build/hal-runner ./build/hal-test.wasm
```

Expected:

```text
platform-info: type=IntelTdx version=0.1.0
list-capabilities: 5 entries
random(32): <64 hex chars>
system-time: <seconds>s <nanoseconds>ns
sha256(hello)=2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
generate-keypair: ok (pub=<n>B priv=<n>B)
```

```bash
./build/hal-runner ./build/attestation-test.wasm --function run-attestation
```

Expected:

```text
platform-info: type=IntelTdx version=0.1.0
attestation: ok (evidence len=<n>)
evidence: 7b226d6561737572656d656e7473223a...
```

The HAL asks the Linux TSM for a DCAP quote, parses `MRTD` and `RTMR0..3` out of it, and returns them as the measurements JSON the WASM side expects — which is why the hex evidence starts with `{"measurements":`. If `attestation` fails while `platform-info` reports `IntelTdx`, the platform was detected but no quote could be produced: check that `/dev/tdx_guest` is present, and that `hal-runner` was built against a wasmhal with the `intel-tdx` feature. If it reports `type=AmdSev` on a TDX VM, the binary predates TDX support — rebuild it.

`hal-runner` embeds the HAL directly rather than going through proplet, so it carries its own wasmhal dependency. Keep it on the same rev as [`proplet/Cargo.toml`](../../proplet/Cargo.toml) — a stale pin is the usual reason the two disagree about what is supported.

## References

- [Trustee](https://github.com/confidential-containers/trustee)
- [Guest Components](https://github.com/confidential-containers/guest-components)
- [Propeller encrypted workloads guide](https://propeller.absmach.eu/docs/tee)
- [GCP Confidential VM supported configurations](https://cloud.google.com/confidential-computing/confidential-vm/docs/supported-configurations)
- [Creating a GCP Confidential VM instance](https://cloud.google.com/confidential-computing/confidential-vm/docs/create-a-confidential-vm-instance)
- [Trustee attestation policies](https://github.com/confidential-containers/trustee/blob/main/attestation-service/docs/policy.md)
- [Azure SEV-SNP CVM guide](../azure/README.md)