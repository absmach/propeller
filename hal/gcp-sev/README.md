# Propeller on an AMD SEV-SNP GCP Confidential VM with Trustee

For the rendered version of this guide, see [propeller.absmach.eu/docs/gcp-sev-cvm](https://propeller.absmach.eu/docs/gcp-sev-cvm).

Deploy [Trustee](https://github.com/confidential-containers/trustee) **outside** the confidential VM and run the Propeller stack with the CoCo **guest
components** **inside** a GCP AMD SEV-SNP confidential VM. This is the production-shaped topology: the relying party (Trustee) verifies evidence and releases keys, and it must not share the trust boundary with the workload it is attesting.

This is the AMD SEV-SNP counterpart of [`../gcp-tdx/README.md`](../gcp-tdx/README.md), which covers Intel TDX on GCP. The two guides share their shape; the platform-specific parts are called out, and the [differences table](#what-is-different-from-the-gcp-intel-tdx-guide) is the place to start if you have already done the TDX one. There is also an [`../azure-sev/README.md`](../azure-sev/README.md) for AMD SEV-SNP on Azure, which is _not_ the same as this: Azure's paravisor hides the hardware and surfaces a vTPM, while GCP exposes the firmware attestation device directly.

## Architecture

| Component                               | Runs on                | Role                                                                    |
| --------------------------------------- | ---------------------- | ----------------------------------------------------------------------- |
| KBS (Key Broker Service)                | Trustee host (outside) | Validates the attestation token, releases decryption keys               |
| AS (Attestation Service)                | Trustee host (outside) | Verifies TEE evidence (here: `snp`)                                     |
| RVPS (Reference Value Provider Service) | Trustee host (outside) | Reference values for verification                                       |
| Attestation Agent (AA)                  | CVM (inside)           | Reads the SNP report from `/dev/sev-guest`, performs the RCAR handshake |
| CoCo Keyprovider                        | CVM (inside)           | Image key-wrap/unwrap keyprovider for `image-rs`                        |
| Proplet                                 | CVM (inside)           | Pulls and runs the encrypted workload                                   |
| Wasmtime                                | CVM (inside)           | Executes the decrypted WASM                                             |

### Where to run Trustee

Trustee runs on a **separate GCE VM you control** — the _Trustee host_ — never inside the CVM and never on the Google host. The whole point of a CVM is that the machine hosting it (and its operator) is untrusted, so the component that verifies evidence and releases decryption keys must be its own trust domain. Put the Trustee host in the same VPC network as the CVM and have the CVM reach KBS over the private network; do not expose KBS publicly.

| Placement                                                 | Correct?                                   |
| --------------------------------------------------------- | ------------------------------------------ |
| A separate GCE VM in the same VPC (internal IP)           | Yes                                        |
| Any machine or server you control, reachable from the CVM | Yes                                        |
| Inside the CVM                                            | No — the keys would live with the workload |
| The CVM's Google host / hypervisor                        | No — you do not control it                 |

Provisioning is in [Part 2](#part-2--deploy-trustee-outside-the-cvm).

## What is different from the GCP Intel TDX guide

|                          | GCP Intel TDX ([`../gcp`](../gcp-tdx/README.md)) | GCP AMD SEV-SNP (this guide)                                 |
| ------------------------ | ------------------------------------------------ | ------------------------------------------------------------ |
| Enable the platform      | `--confidential-compute-type=TDX`                | `--confidential-compute-type=SEV_SNP`                        |
| Machine family           | `c3-standard-*` (`c4-standard-*` preview)        | `n2d-standard-*` (`c4d-standard-*` preview)                  |
| CPU platform flag        | not needed                                       | `--min-cpu-platform="AMD Milan"` (or newer)                  |
| Attester                 | `tdx-attester`                                   | `snp-attester`                                               |
| Evidence source          | DCAP quote from the Linux TSM (`/dev/tdx_guest`) | firmware SNP report from `/dev/sev-guest`, via the Linux TSM |
| Guest device to check    | `/dev/tdx_guest`                                 | `/dev/sev-guest`                                             |
| Guest verification       | `Memory Encryption Features active: TDX`         | `AMD Memory Encryption Features active: SEV-SNP`             |
| Boot chain measured into | RTMRs                                            | the SNP report's measurement (guest firmware)                |
| HAL `platform-info` type | `IntelTdx`                                       | `AmdSev`                                                     |

Three things are worth saying plainly, because they cause most of the confusion here:

- **GCP SEV-SNP is not Azure SEV-SNP.** On Azure the paravisor hides `/dev/sev-guest` and the evidence is a vTPM HCL report plus a TPM quote, shaped for Trustee's `az-snp-vtpm` verifier. On GCP the firmware device is present, the evidence is a signed SNP attestation report, and Trustee's plain `snp` verifier consumes it. Do not carry the Azure attester (`az-snp-vtpm-attester`) over — it will find no Azure HCL and fall through.
- **The HAL's evidence shape on GCP SEV matches TDX, not Azure.** On this platform the HAL returns the same `{"measurements": …}` document as the TDX path, because that shape is what the WIT `attestation` host call promises to WASM. The Azure vTPM path returns a different (CoCo `az_snp_vtpm`) shape. If you compare Part 6 output between the Azure and GCP SEV guides and they differ, that is why, and both are correct for their platform.
- **A `/dev/tpm0` on the guest is not the attestation path.** If the instance was created from the console it likely carries `--shielded-vtpm`, so a vTPM exists. Nothing here reads it — and never add `tpm-attester` to the guest build because of it.

## Prerequisites

- **CVM**: a GCP project with quota for the `n2d` machine family in a SEV-SNP zone, `gcloud` configured, and an SSH key.
- **Trustee host**: a plain GCE VM (no confidential SKU required), provisioned in [Part 2.1](#21-provision-the-trustee-host) in the same VPC as the CVM. Docker and Docker Compose installed.
- **Client tools** (on the Trustee host): `cargo` (for `kbs-client`), `skopeo`, `wasm-to-oci`.
- The CVM must reach the Trustee host on the KBS port (default `8082`) over the private network. A default VPC network already permits this.

## Part 1 — Provision the AMD SEV-SNP CVM

Set the project and zone once and keep the same shell for the rest of the guide:

```bash
gcloud auth login
gcloud config set project <project-id>
PROJECT=<project-id>
ZONE=us-central1-a

gcloud compute instances create propeller-amd-sev-cvm \
  --project "$PROJECT" \
  --zone "$ZONE" \
  --machine-type=n2d-standard-4 \
  --min-cpu-platform="AMD Milan" \
  --confidential-compute-type=SEV_SNP \
  --maintenance-policy=TERMINATE \
  --provisioning-model=STANDARD \
  --network-interface=network-tier=PREMIUM,nic-type=GVNIC,stack-type=IPV4_ONLY,subnet=default \
  --tags=propeller \
  --create-disk=auto-delete=yes,boot=yes,mode=rw,size=40,type=pd-balanced,image-family=ubuntu-2404-lts-amd64,image-project=ubuntu-os-cloud \
  --shielded-integrity-monitoring
```

These flags are what make the CVM work:

| Flag                                  | Why it is there                                                                                                                                  |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `--machine-type=n2d-standard-*`       | AMD SEV-SNP is offered on the N2D family (AMD Milan). `c4d-standard-*` (Turin) is the other option, currently in preview.                        |
| `--min-cpu-platform="AMD Milan"`      | Pins the instance to a CPU generation that supports SEV-SNP. Older N2D hosts (Rome) do not, and the create is rejected without it.               |
| `--confidential-compute-type=SEV_SNP` | This is what puts the VM in a confidential domain. Without it the same machine type is an ordinary N2D VM with no `/dev/sev-guest`.              |
| `--maintenance-policy=TERMINATE`      | Live migration is not supported for confidential instances; `gcloud` rejects the create without it.                                              |
| `--create-disk=…image-family=…`       | The image must be tagged `SEV_SNP_CAPABLE`. The `ubuntu-os-cloud` and Ubuntu Pro families are; a kernel without the SEV-SNP guest driver is not. |

`--shielded-integrity-monitoring` is optional and harmless. Secure Boot and a vTPM are **not** required on this platform, unlike Azure — the firmware device is the evidence source. If your project enables them anyway, see the third note in the [differences section](#what-is-different-from-the-gcp-intel-tdx-guide).

### 1.1 Pick a zone that has SEV-SNP

The authoritative list is [Supported configurations](https://cloud.google.com/confidential-computing/confidential-vm/docs/supported-configurations). At the time of writing SEV-SNP on `n2d-standard-*` is offered in `us-central1-a/b/c`, `us-east` and `europe-west` zones, and `asia-southeast1`. Check a candidate zone from the CLI:

```bash
gcloud compute machine-types list \
  --filter="name=n2d-standard-4 AND zone:~$ZONE" \
  --format="value(name)"
```

Being offered is necessary but not sufficient — a plain (non-confidential) `n2d-standard-4` is the same machine type. If the create fails with a confidential-compute error, pick another zone from the table.

Confirm the image supports SEV-SNP:

```bash
gcloud compute images list \
  --filter="guestOsFeatures[].type:(SEV_SNP_CAPABLE)" \
  --format="table(name,family,project)"
```

### 1.2 Connect and record the addresses

A stock GCP image has no `propeller` user, so create one and install your key — the rest of the guide assumes you SSH as `propeller`:

```bash
CVM_IP=$(gcloud compute instances describe propeller-amd-sev-cvm \
  --zone "$ZONE" --format='get(networkInterfaces[0].accessConfigs[0].natIP)')

# from your own machine or an existing admin session on the instance
sudo adduser --disabled-password --gecos "" propeller
sudo adduser propeller sudo
sudo install -d -m 700 -o propeller -g propeller /home/propeller/.ssh
sudo tee -a /home/propeller/.ssh/authorized_keys < ~/.ssh/cloud.pub
sudo chown propeller:propeller /home/propeller/.ssh/authorized_keys

# passwordless sudo — required, see below
echo 'propeller ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/90-propeller
sudo chmod 0440 /etc/sudoers.d/90-propeller

ssh -i ~/.ssh/cloud propeller@$CVM_IP
```

The sudoers line is not optional. `--disabled-password` gives the account an **empty** password, and `sudo` will not accept an empty one, so without that line every `sudo` in this guide re-prompts and fails:

```text
[sudo: authenticate] Password:
sudo: Authentication failed, try again.
```

This is how the stock images behave for their own login user, which is why `gcloud compute ssh` needs no password: the guest agent installs a sudoers drop-in. Recreating that one line is all it takes.

`gcloud compute ssh propeller-amd-sev-cvm --zone "$ZONE"` is the shortcut that skips the block, under your Google account name — use that name in place of `propeller` below if you take it.

Use the instance's **external** IP for SSH, and its **internal** IP for everything the guest dials (KBS in Part 3).

### 1.3 Confirm the guest is really in a confidential domain

A plain N2D VM and a SEV-SNP CVM are the same machine type, so nothing outside the guest distinguishes them — the flag does, and this is where you confirm it:

```bash
sudo dmesg | grep -i -e sev -e "Memory Encryption"
# expect: AMD Memory Encryption Features active: SEV-SNP

ls -l /dev/sev-guest
# expect: /dev/sev-guest present

ls -d /sys/kernel/config/tsm /sys/kernel/config/tsm/report 2>/dev/null
# the TSM configfs interface the attester and the HAL use to request a report
```

`/dev/sev-guest` and the TSM are the two things that must be there. Everything else on this guest can be absent or present without affecting attestation.

If `SEV-SNP` is missing from the `dmesg` line — in particular if it says only `SEV` or `SEV-ES` — the VM is not running with SNP enabled: delete it and re-create with `--confidential-compute-type=SEV_SNP` and `--min-cpu-platform="AMD Milan"`.

If `/dev/sev-guest` is missing while the kernel reports SEV-SNP, the guest kernel lacks the driver (`CONFIG_SEV_GUEST`); use a different, `SEV_SNP_CAPABLE` image.

## Part 2 — Deploy Trustee outside the CVM

All of this runs on `TRUSTEE_HOST`, never inside the CVM. It is identical to the Intel TDX guide; the commands are repeated here so this document stands alone, and the reasoning (why HTTP, why the token CA, why the client build avoids Intel's repo) is in [`../gcp-tdx/README.md` Part 2](../gcp-tdx/README.md#part-2--deploy-trustee-outside-the-cvm).

### 2.1 Provision the Trustee host

```bash
gcloud compute instances create propeller-trustee-host \
  --project "$PROJECT" \
  --zone "$ZONE" \
  --machine-type=e2-standard-2 \
  --network-interface=network-tier=PREMIUM,nic-type=GVNIC,stack-type=IPV4_ONLY,subnet=default \
  --provisioning-model=STANDARD \
  --tags=propeller \
  --image-family=ubuntu-2404-lts-amd64 \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=40G \
  --boot-disk-type=pd-balanced \
  --boot-disk-auto-delete
```

Note the absence of `--maintenance-policy=TERMINATE`: a plain `e2` instance is live-migrated on host maintenance, and asking it to terminate is rejected (`e2 instances do not support onHostMaintenance=TERMINATE unless they are preemptible`). `TERMINATE` is for the confidential CVM.

Any current LTS works. The only thing built on this host is `kbs-client`, and [2.5](#25-create-and-upload-the-image-key) keeps it free of Intel's SGX/DCAP native dependencies, so there is no constraint on the host's Ubuntu release.

Record both addresses — the guest dials the **internal** one, you SSH to the **external** one:

```bash
TRUSTEE_INTERNAL=$(gcloud compute instances describe propeller-trustee-host \
  --zone "$ZONE" --format='get(networkInterfaces[0].networkIP)')
TRUSTEE_EXTERNAL=$(gcloud compute instances describe propeller-trustee-host \
  --zone "$ZONE" --format='get(networkInterfaces[0].accessConfigs[0].natIP)')

export TRUSTEE_HOST=$TRUSTEE_INTERNAL
export TRUSTEE_SSH=$TRUSTEE_EXTERNAL
export CVM_HOST=$CVM_IP
```

In a default VPC the CVM can already reach `8082`: `default-allow-internal` permits all TCP from `10.128.0.0/9`, and both instances sit in that range. Add an explicit rule only if you want the permission to be visible rather than implied:

```bash
gcloud compute firewall-rules create allow-kbs-from-propeller \
  --project "$PROJECT" \
  --allow=tcp:8082 \
  --source-tags=propeller \
  --target-tags=propeller \
  --network=default \
  --direction=INGRESS \
  --priority=1000
```

Create the `propeller` user on the Trustee host too (same block as [1.2](#12-connect-and-record-the-addresses)), then install Docker:

```bash
ssh -i ~/.ssh/cloud propeller@$TRUSTEE_SSH
sudo apt update && sudo apt upgrade -y
sudo apt install -y git curl
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"   # log out and back in to take effect
sudo systemctl enable docker.service
```

### 2.2 Start the Trustee stack

```bash
git clone https://github.com/confidential-containers/trustee
cd trustee

openssl genpkey -algorithm ed25519 > kbs/config/private.key
openssl pkey -in kbs/config/private.key -pubout -out kbs/config/public.pub
```

KBS listens on container port `8080`; this guide publishes it on `8082`. Check the `kbs` service mapping and change the host side to `8082:8080` if it publishes `8080:8080`:

```bash
awk '/^  kbs:/,/^  [a-z]/' docker-compose.yml | grep -A3 ports
```

`[http_server]` in `kbs/config/docker-compose/kbs-config.toml` must read `insecure_http = true` with `sockets = ["0.0.0.0:8080"]` — already the shipped default. Then:

```bash
docker compose up -d
docker compose ps
```

KBS runs over plain HTTP inside the VPC. `/etc/attestation-agent.toml` on the guest trusts no certificate, and `coco_keyprovider` cannot be pointed at a private CA through `SSL_CERT_FILE`; the full reasoning is in [`../gcp-tdx/README.md` §2.2](../gcp-tdx/README.md#22-start-the-trustee-stack).

### 2.3 Check that KBS trusts the Attestation Service's token CA

`setup.sh` generates two PKI hierarchies in one directory; KBS must verify the AS-issued token against the **token** CA, not the TLS CA. Current Trustee already points `trusted_certs_paths` at `ca-cert.pem`, which is exactly the token-signing root. Confirm rather than assume:

```bash
cd ~/trustee/kbs/config/docker-compose
grep -A2 '\[attestation_token\]' kbs-config.toml
# expect: trusted_certs_paths = ["/opt/confidential-containers/kbs/user-keys/ca-cert.pem"]

awk '/BEGIN CERTIFICATE/,/END CERTIFICATE/' token-cert-chain.pem \
  | awk 'BEGIN{n=0} /BEGIN CERTIFICATE/{n++} n==2' > token-ca-cert.pem
openssl verify -CAfile token-ca-cert.pem token-cert.pem
# expect: token-cert.pem: OK
```

If the verify passes and the config already names `ca-cert.pem`, leave it alone. If it names a `tls-*` certificate, or the verify fails:

```bash
cd ~/trustee
sed -i 's|trusted_certs_paths = \[.*\]|trusted_certs_paths = ["/opt/confidential-containers/kbs/user-keys/token-ca-cert.pem"]|' \
  kbs/config/docker-compose/kbs-config.toml
docker compose up -d --force-recreate kbs
```

Getting it wrong fails in the guest with `TokenVerifierError: Cannot verify token: neither trusted jwk set nor trusted pem public key works`.

### 2.4 Verify KBS is reachable

```bash
curl -s -o /dev/null -w 'http=%{http_code}\n' http://${TRUSTEE_HOST}:8082/kbs/v0/resource-policy
# expect http=401 — a JSON auth error, not a connection error
```

### 2.5 Create and upload the image key

`kbs-client` is built from the Trustee tree, and it is the only thing you need to build on this host — the KBS and AS run from prebuilt images via the compose file. Install a plain Rust toolchain:

```bash
sudo apt-get update
sudo apt-get install -y \
  build-essential gcc make pkg-config libssl-dev openssl curl git \
  clang cmake jq unzip protobuf-compiler

curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
. "$HOME/.cargo/env"
```

**No Intel SGX or DCAP packages are needed, for SEV or for TDX.** This trips people up because the obvious command fails: at the workspace root, a bare `cargo build --release` compiles every member, including the `verifier` crate, whose default `all-verifier` feature enables the TDX and SGX quote verifiers. Both need `intel-tee-quote-verification-sys`, whose build script wants `sgx_dcap_quoteverify.h`:

```text
error: failed to run custom build command for `intel-tee-quote-verification-sys v0.3.0`
bindings.h:32:10: fatal error: 'sgx_dcap_quoteverify.h' file not found
```

The header ships in Intel's `libsgx-dcap-quote-verify-dev`, which is not in Ubuntu, so the apparent fix is to add Intel's apt repository. That works, but it installs an SGX verifier this deployment never runs — the AS that actually verifies your SNP evidence runs from a prebuilt image with its own verifiers. The dependency shows up only because the workspace build compiles a crate you are not going to use.

Building the client alone avoids it entirely:

```bash
cd ~/trustee
cargo build --release -p kbs-client
ls -l target/release/kbs-client
```

`kbs-client` depends on `kbs_protocol` and on nothing from `attestation-service` or `verifier`, so its graph has no `intel-tee-quote-verification-sys` and no `tss-esapi`; it compiles on a stock Ubuntu with none of Intel's packages. Only if you build the whole workspace or the AS from source do you need Intel's repository — then follow Intel's own instructions, since both `jammy` and `noble` carry the packages.

```bash
openssl rand -base64 32 | tr -d '\n' > private_key

./target/release/kbs-client \
  --url http://${TRUSTEE_HOST}:8082 \
  config set-resource \
  --resource-file private_key \
  --path default/key/propeller-addition
```

You are the **admin** here, so this only needs the KBS address — the default `admin` authorization in `kbs-config.toml` accepts it. Over HTTP there is no `--cert-file`.

### 2.6 Set the resource policy

```bash
./target/release/kbs-client \
  --url http://${TRUSTEE_HOST}:8082 \
  config set-resource-policy \
  --policy-file kbs/sample_policies/allow_all.rego
```

`allow_all` is for the first run only — tighten it once the flow works, see [Tightening the attestation policy](#tightening-the-attestation-policy).

## Part 3 — Guest stack inside the CVM

Everything in this part runs inside the confidential VM.

### 3.1 Install build dependencies and runtimes

```bash
sudo apt install -y git curl
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"
sudo systemctl enable docker.service

sudo apt-get update
sudo apt-get install -y \
  build-essential gcc make pkg-config libssl-dev openssl curl git \
  clang cmake jq unzip netcat-openbsd libtss2-dev

curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
. "$HOME/.cargo/env"

WASMTIME_VERSION=v48.0.2
curl -L "https://github.com/bytecodealliance/wasmtime/releases/download/${WASMTIME_VERSION}/wasmtime-${WASMTIME_VERSION}-x86_64-linux.tar.xz" -o /tmp/wasmtime.tar.xz
tar -xf /tmp/wasmtime.tar.xz -C /tmp
sudo mv /tmp/wasmtime-${WASMTIME_VERSION}-x86_64-linux/wasmtime /usr/local/bin/
rm -rf /tmp/wasmtime*
wasmtime --version
```

`libtss2-dev` looks out of place on a SEV guest and is worth explaining: the SNP attester reads `/dev/sev-guest` and requests its report through the TSM, so no TPM is involved. The TSS dependency comes from **proplet's Azure support**, compiled in twice over: `proplet/Cargo.toml` enables `az-snp-vtpm-attester` so one proplet binary also runs on Azure, and `elastic-tee-hal`'s `amd-sev` feature pulls the same Azure vTPM crates for the HAL. Both reach `tss-esapi`, which has no bundled TSS and must find the system `tss2-sys`:

```text
error: failed to run custom build command for `tss-esapi-sys v0.6.0`
Failed to find tss2-sys library.:
  Package tss2-sys was not found in the pkg-config search path.
```

Install the headers, or build from crates that exclude the Azure path. No `tpm2-tools` and no `libsgx-dcap-*` are needed: nothing here reads `/dev/tpm0`, and the DCAP headers are a Trustee-host concern only if you build the whole Trustee workspace.

### 3.2 Build Attestation Agent and CoCo Keyprovider with the SNP attester

Build with **`snp-attester`**, and only that one:

```bash
cd /tmp
git clone https://github.com/rodneyosodo/guest-components.git
cd guest-components
git checkout upstream-proplet-rebased

# Attestation Agent (gRPC) with the AMD SEV-SNP attester only
cd attestation-agent
make ATTESTER=snp-attester ttrpc=false
sudo make install

# CoCo Keyprovider — same attester; do NOT use the default (all-attesters)
cd coco_keyprovider
cargo build --release --target x86_64-unknown-linux-gnu \
  --no-default-features --features snp-attester
sudo cp ../../target/x86_64-unknown-linux-gnu/release/coco_keyprovider /usr/local/bin/

attestation-agent --help >/dev/null && echo "AA OK"
coco_keyprovider --help >/dev/null && echo "keyprovider OK"
```

**Check out `upstream-proplet-rebased`, not `enable-wasm-workloads`.** On the wrong branch the keyprovider has no features at all and `un_wrap_key` returns `Status::unimplemented`, so it builds and installs cleanly and only fails at decrypt time. The two symptoms are:

```text
error: the package 'coco_keyprovider' does not contain this feature: snp-attester
```

```text
UnWrapKey API is unimplemented!
```

Two more things not to do:

- **Do not use `all-attesters`.** It enables the NVIDIA attester, whose build script needs the NVIDIA C++ attestation SDK and fails. `ATTESTER=snp-attester` replaces `all-attesters`, and the keyprovider needs `--no-default-features`.
- **Do not add `tpm-attester`.** With a generic TPM attester compiled in, `detect_attestable_devices()` reports the TPM as an _additional_ device and composite evidence demands a quote that says nothing about the confidential domain. This guest may well have a vTPM (Shielded VM), which makes it tempting; it is not the evidence source here.

### 3.3 Build Proplet

```bash
git clone https://github.com/absmach/propeller.git ~/propeller
cd ~/propeller/proplet
cargo build --release
sudo cp target/release/proplet /usr/local/bin/
```

No feature flags. `proplet/Cargo.toml` compiles `snp-attester` next to `az-snp-vtpm-attester` and `tdx-attester`, so one binary serves Azure SEV-SNP (vTPM evidence), GCP/bare-metal Intel TDX (TSM quote) and GCP/bare-metal AMD SEV-SNP (TSM report); the running host decides which is primary. Confirm before building:

```bash
grep -c snp-attester Cargo.toml     # expect 2
```

If it prints `0`, your clone predates SNP support and a build from it will start cleanly while quietly using the sample attester — pull a tree that has it.

### 3.4 Write the Attestation Agent config

The config file must have a `.toml` (or `.json`) extension, or the AA fails to start with `is not of a supported file format`:

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

`enable_eventlog = false` is correct here: the SEV-SNP runtime measurement is in the firmware report, not a TPM PCR, so there is no eventlog for the AA to extend. `init_pcr` is inert while that is off.

Verify the guest can reach KBS:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://${TRUSTEE_HOST}:8082/kbs/v0/resource-policy
# expect 401
```

### 3.5 Run the services

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
PROPLET_HAL_ENABLED=true
PROPLET_KBS_URI=http://${TRUSTEE_HOST}:8082
PROPLET_AA_CONFIG_PATH=/etc/attestation-agent.toml
PROPLET_LAYER_STORE_PATH=/tmp/proplet/layers
EOF
```

**`PROPLET_HAL_ENABLED=true` and `PROPLET_EXTERNAL_WASM_RUNTIME` are mutually exclusive.** The external runtime is a separate `wasmtime` process and the HAL is an in-process host, so setting both logs `PROPLET_HAL_ENABLED is set but the external Wasm runtime does not support the HAL; ignoring it` and silently disables the HAL. This template takes the in-process path, which [Part 6](#part-6--test-the-hal-interfaces-directly) needs; if you only run plain WASM tasks, drop the `PROPLET_HAL_ENABLED` line and set the external runtime instead.

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

# Unquoted heredoc: this is the unit that needs $TRUSTEE_HOST expanded into it.
sudo tee /etc/systemd/system/coco-keyprovider.service >/dev/null <<EOF
[Unit]
Description=CoCo Keyprovider for Confidential Containers
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStartPre=/bin/mkdir -p /run/coco-keyprovider
ExecStart=/usr/local/bin/coco_keyprovider --socket 127.0.0.1:50011 --kbs http://${TRUSTEE_HOST}:8082
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
systemctl cat coco-keyprovider | grep ExecStart
# expect a real IP, not $TRUSTEE_HOST or <TRUSTEE_HOST>
```

A literal placeholder in that ExecStart is not cosmetic: `un_wrap_key` reads only that argument, so the keyprovider starts, accepts the gRPC call, and fails every decryption with `KBS URL not configured. Please provide KBS URL to keyprovider.`

### 3.6 Watch the services

```bash
sudo journalctl -u attestation-agent -f
sudo journalctl -u coco-keyprovider -f
sudo journalctl -u proplet -f
```

`proplet.service` has `Requires=coco-keyprovider.service`, so stopping the keyprovider stops proplet — restart both together.

### 3.7 What a healthy proplet start looks like

```text
ELASTIC TEE HAL initialized for platform: AmdSev
  - AMD SEV-SNP report device: /dev/sev-guest
  - TSM support: available
Detected system limits: … (TEE: TDX=false, SEV=true)
TEE runtime initialized successfully
```

Check the attester specifically, since `TEE: … SEV=true` is Proplet's own detection and is independent of it:

```bash
sudo systemctl restart proplet
sudo journalctl -u proplet --since "1 min ago" | grep -E "TEE|TPM|Attester"
```

A correct build shows the two `TEE` lines and nothing about TPMs or an attester. If you also see `No TEE platform detected. Sample Attester will be used.` the binary was built without `snp-attester` ([3.3](#33-build-proplet)).

A `WARNING:esys:…Esys_NV_ReadPublic…` line can still appear here if the guest has a vTPM, because the compiled-in Azure attester (`az-snp-vtpm-attester`) probes it before the SNP attester is reached. It is noise, not a failure, and the SNP attester runs next — unlike the TDX case there is nothing before it that matters, so it does not change the outcome.

## Part 4 — Encrypt and publish a WASM image

These commands run on the **Trustee host** and are identical to [`../gcp-tdx/README.md` Part 4](../gcp-tdx/README.md#part-4--encrypt-and-publish-a-wasm-image).

```bash
wget https://github.com/tinygo-org/tinygo/releases/download/v0.42.0/tinygo_0.42.0_amd64.deb
sudo dpkg -i tinygo_0.42.0_amd64.deb && rm -f tinygo_0.42.0_amd64.deb

wget https://github.com/engineerd/wasm-to-oci/releases/download/v0.1.2/linux-amd64-wasm-to-oci
sudo mv linux-amd64-wasm-to-oci /usr/local/bin/wasm-to-oci
sudo chmod +x /usr/local/bin/wasm-to-oci

cd ~/propeller
GOOS=js GOARCH=wasm tinygo build -buildmode=c-shared -o build/addition.wasm -target wasi examples/addition/addition.go

docker login docker.io
wasm-to-oci push build/addition.wasm docker.io/rodneydav/gcp-amd-tee-wasm-addition:latest --server docker.io

cd ~/trustee
mkdir -p output
docker run \
  -v "$PWD/output:/output" \
  docker.io/rodneydav/coco-keyprovider:latest \
  /encrypt.sh \
  -k "$(cat ./private_key)" \
  -i kbs:///default/key/propeller-addition \
  -s docker://docker.io/rodneydav/gcp-amd-tee-wasm-addition:latest \
  -d dir:/output

sudo apt install -y skopeo
skopeo login docker.io
skopeo copy dir:$(pwd)/output docker://docker.io/rodneydav/gcp-amd-tee-wasm-addition:encrypted
```

`skopeo copy` pushes directly; do not follow it with `docker push docker://…`, which rejects the scheme.

## Part 5 — Run an encrypted workload and verify

Submit a task — `image_url` is a bare OCI reference with no `docker://`, and there is no `file` field:

```bash
curl -s -X POST http://localhost:7070/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "add",
    "image_url": "rodneydav/gcp-amd-tee-wasm-addition:encrypted",
    "encrypted": true,
    "kbs_resource_path": "default/key/propeller-addition",
    "cli_args": ["--invoke", "add"],
    "inputs": [10, 20]
  }'
```

```bash
TID=<task id>
curl -s -X POST "http://localhost:7070/tasks/$TID/start"
sleep 15
curl -s "http://localhost:7070/tasks/$TID" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('state:',d['state']); print('results:',repr(d.get('results'))); print('error:',d.get('error'))"
```

`state: 3` with `results: '30\n'` is success — `10 + 20`, decrypted and executed inside the confidential domain.

A successful run means the whole chain worked: the AA read a signed SNP report from `/dev/sev-guest`, the AS verified it with the `snp` verifier, KBS released the key, the keyprovider unwrapped it, and `image-rs` decrypted the layer inside the CVM.

### Tightening the attestation policy

`allow_all` releases the key to any guest that can reach KBS. To require a genuine SEV-SNP guest, Trustee's `default_cpu` policy takes its `input.snp` branch, which compares the report's `reported_tcb_*` and measurement fields against reference values you upload through the RVPS, plus a resource policy that inspects the resulting token. Unlike the TDX case on GCP there is no GRUB-versus-TDVF split to reason about, but the same rule applies: the reference values track the guest firmware and change when it does. Treat this as the step after the end-to-end flow works.

## Part 6 — Test the HAL interfaces directly

Run this **in the CVM**; on any host without a TEE — the Trustee host included — `hal-runner` reports the stub and attestation fails by design.

```bash
cd ~/propeller
rustup target add wasm32-wasip2
make hal-runner hal-test attestation-test

./build/hal-runner ./build/hal-test.wasm
```

Expected:

```text
platform-info: type=AmdSev version=0.1.0
list-capabilities: 5 entries
random(32): cbae6cfb7624bb46a3b1591f586274293482e567871ec88f5f9b5d012a86e5f4
system-time: 1791231481s 468526634ns
sha256(hello)=2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
generate-keypair: ok (pub=32B priv=32B)
```

```bash
./build/hal-runner ./build/attestation-test.wasm --function run-attestation
```

Expected:

```text
platform-info: type=AmdSev version=0.1.0
attestation: ok (evidence len=314)
evidence: 7b226d6561737572656d656e7473223a7b22626f6f746c6f61646572223a22706c616365686f6c6465725f68617368222c2268616c223a22706c616365686f6c6465725f68617368222c226b65726e656c223a22706c616365686f6c6465725f68617368227d2c22706c6174666f726d223a22616d642d7365762d736e70222c227265706f72745f64617461223a223637383336353161653363626233313664613662353734653861383731633731353534363063633639383934323063386666393466363739626135313165366130303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030222c2274696d657374616d70223a313739313233313439352c2276657273696f6e223a22302e312e30227d
```

The HAL requests an SNP report through the TSM, parses the measurement and reported TCB out of it, and returns them as the measurements JSON the WASM side expects — which is why the hex evidence starts with `{"measurements":`. This is deliberately the _same shape_ as the TDX path and a _different shape_ from the Azure vTPM path; see the [differences section](#what-is-different-from-the-gcp-intel-tdx-guide). If `attestation` fails while `platform-info` reports `AmdSev`, check `/dev/sev-guest` and that `hal-runner` was built against a wasmhal with the SNP-TSM path.

## Part 7 — Delete the resources

From your workstation, in the shell that still has `$PROJECT` and `$ZONE` — not from inside either VM:

```bash
gcloud compute instances delete propeller-amd-sev-cvm --zone "$ZONE" --quiet
gcloud compute instances delete propeller-trustee-host  --zone "$ZONE" --quiet

gcloud compute firewall-rules delete allow-kbs-from-propeller \
  --project "$PROJECT" --quiet     # if you created it

gcloud compute instances list --filter="name~propeller"
gcloud compute disks     list --filter="name~propeller"
```

Both boot disks were created with auto-delete, so they go with their instances; a surviving disk means the console created it without `auto-delete=yes`. The KBS signing keys and the uploaded image key live in `~/trustee` on the Trustee host and are discarded with it — nothing else can read the encrypted image at that point, which is the intent. The encrypted image in the registry is not deleted by any of this.

## References

- [Trustee](https://github.com/confidential-containers/trustee)
- [Guest Components](https://github.com/confidential-containers/guest-components)
- [Propeller encrypted workloads guide](https://propeller.absmach.eu/docs/tee)
- [GCP Confidential VM supported configurations](https://cloud.google.com/confidential-computing/confidential-vm/docs/supported-configurations)
- [Creating a GCP Confidential VM instance](https://cloud.google.com/confidential-computing/confidential-vm/docs/create-a-confidential-vm-instance)
- [Trustee attestation policies](https://github.com/confidential-containers/trustee/blob/main/attestation-service/docs/policy.md)
- [GCP Intel TDX CVM guide](../gcp-tdx/README.md)
- [Azure SEV-SNP CVM guide](../azure-sev/README.md)
