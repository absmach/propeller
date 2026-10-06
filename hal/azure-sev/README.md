# Propeller on an AMD SEV-SNP Azure CVM with Trustee

For the rendered version of this guide, see [propeller.absmach.eu/docs/azure-cvm](https://propeller.absmach.eu/docs/azure-cvm).

Deploy [Trustee](https://github.com/confidential-containers/trustee) **outside** the confidential VM and run the Propeller stack with the CoCo **guest
components** **inside** an Azure AMD SEV-SNP confidential VM. This is the production-shaped topology: the relying party (Trustee) verifies evidence and releases keys, and it must not share the trust boundary with the workload it is attesting.

For the Intel TDX equivalent on GCP, see [`../gcp-tdx/README.md`](../gcp-tdx/README.md). Parts 4–6 of the two guides are the same; the platform differences are collected in that guide's [comparison table](../gcp-tdx/README.md#what-is-different-from-the-azure-amd-sev-snp-guide).

## Architecture

| Component                               | Runs on                | Role                                                         |
| --------------------------------------- | ---------------------- | ------------------------------------------------------------ |
| KBS (Key Broker Service)                | Trustee host (outside) | Validates the attestation token, releases decryption keys    |
| AS (Attestation Service)                | Trustee host (outside) | Verifies TEE evidence (here: `az-snp-vtpm`)                  |
| RVPS (Reference Value Provider Service) | Trustee host (outside) | Reference values for verification                            |
| Attestation Agent (AA)                  | CVM (inside)           | Collects evidence from the vTPM, performs the RCAR handshake |
| CoCo Keyprovider                        | CVM (inside)           | Image key-wrap/unwrap keyprovider for `image-rs`             |
| Proplet                                 | CVM (inside)           | Pulls and runs the encrypted workload                        |
| Wasmtime                                | CVM (inside)           | Executes the decrypted WASM                                  |

### Where to run Trustee

Trustee runs on a **separate Azure VM you control** — the _Trustee host_ — never inside the CVM and never on the Azure hypervisor. The whole point of a CVM is that the machine hosting it (and its operator) is untrusted, so the component that verifies evidence and releases decryption keys must be its own trust domain. Put the Trustee host in the same virtual network as the CVM and have the CVM reach KBS over the private network; do not expose KBS publicly.

| Placement                                                 | Correct?                                   |
| --------------------------------------------------------- | ------------------------------------------ |
| A separate Azure VM in the same vNet (private IP)         | Yes                                        |
| Any machine or server you control, reachable from the CVM | Yes                                        |
| Inside the CVM                                            | No — the keys would live with the workload |
| The CVM's Azure host / hypervisor                         | No — you do not control it                 |

Provisioning is in [Part 2](#part-2--deploy-trustee-outside-the-cvm).

## Prerequisites

- **CVM**: Azure subscription with quota for the `DCasv5` family (usually 0 by default — request it), Azure CLI 2.38.0+, an SSH key.
- **Trustee host**: a plain Azure VM (no confidential SKU required), provisioned in [Part 2.1](#21-provision-the-trustee-host) in the same virtual network as the CVM. Docker and Docker Compose installed.
- **Client tools** (on the Trustee host): `cargo` (for `kbs-client`), `skopeo`, `wasm-to-oci`.
- The CVM must reach the Trustee host on the KBS port (default `8082`) over the private network. The NSG rule allowing this is created in [Part 2.1](#21-provision-the-trustee-host).

Both VMs are created from scratch below, in one resource group.

## Part 1 — Provision the AMD CVM

Everything from here builds a working deployment from scratch. Set the resource group and region once and keep the same shell for the rest of the guide:

```bash
az login
RG=propeller-hal-rg
LOCATION=eastus

# DCasv5 availability varies by region — check before creating:
#   az vm list-skus --location "$LOCATION" --size Standard_DC --output table
az group create --name "$RG" --location "$LOCATION"

# These three flags are what make the CVM work. See the notes below.
az vm create \
  --resource-group "$RG" \
  --name propeller-amd-cvm \
  --size Standard_DC2as_v5 \
  --image "Canonical:ubuntu-26_04-lts:server-cvm:latest" \
  --admin-username propeller \
  --ssh-key-values ~/.ssh/cloud.pub \
  --security-type ConfidentialVM \
  --enable-vtpm true \
  --enable-secure-boot true \
  --os-disk-security-encryption-type VMGuestStateOnly \
  --public-ip-sku Standard
```

`--ssh-key-values ~/.ssh/cloud.pub` installs your existing public key instead of generating a new one with `--generate-ssh-keys`. Then SSH with the matching private key:

```bash
ssh -i ~/.ssh/cloud propeller@$CVM_IP
```

The security flags are mandatory, not optional:

| Flag                             | Why it is required                                                                                                                                                                                                         |
| -------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--security-type ConfidentialVM` | Selects AMD SEV-SNP on the `DCasv5`/`ECasv5` families. Without it the same SKU produces a regular, non-confidential VM and there is no evidence at all.                                                                    |
| `--enable-vtpm true`             | Gives the guest a virtual TPM. The `az-snp-vtpm` attester reads the vTPM (`/dev/tpm0`) to produce evidence. A `ConfidentialVM` without a vTPM still boots, but every attestation call fails and KBS has nothing to verify. |
| `--enable-secure-boot true`      | Required with the CVM image. Secure Boot measures the boot chain into the vTPM, which binds the evidence to the image you intended to run.                                                                                 |

```bash
az vm image list --publisher Canonical --all --output table | grep -i cvm
```

- `VMGuestStateOnly` encrypts only the VM guest state; use `DiskWithVMGuestState` if you also want confidential OS disk encryption. Either works for attestation.

Record the CVM's private IP and confirm the security settings took effect:

```bash
CVM_IP=$(az vm show -g "$RG" -n propeller-amd-cvm -d --query privateIps -o tsv)
CVM_IP=$(az vm show -g "$RG" -n propeller-amd-cvm -d --query publicIps -o tsv)

az vm show -g "$RG" -n propeller-amd-cvm \
  --query "securityProfile" -o json
```

The output must show all three:

```json
{
  "securityType": "ConfidentialVM",
  "uefiSettings": {
    "secureBootEnabled": true,
    "vTpmEnabled": true
  }
}
```

If `vTpmEnabled` is `false`, the VM cannot produce evidence — delete it and re-create with `--enable-vtpm true`.

Confirm from inside the CVM before continuing:

```bash
ssh -i ~/.ssh/cloud propeller@$CVM_IP

sudo dmesg | grep -i -e sev -e "confidential virtualization"
ls -l /dev/tpm* /dev/tpmrm* 2>&1
sudo systemctl status systemd-tpm2-setup.service --no-pager    # vTPM setup
```

Expected: `Detected confidential virtualization sev-snp`, a vTPM device (`/dev/tpm0` and/or `/dev/tpmrm0`), and a successful `systemd-tpm2-setup` service. The absence of `/dev/sev*` is normal — the vTPM is the evidence source.

The CVM also carries the **GuestAttestation** extension (`Microsoft.Azure.Security.LinuxAttestation`), which Azure attaches automatically; it is what talks to Microsoft Azure Attestation. It is not used by Trustee, which verifies the raw vTPM evidence itself.

## Part 2 — Deploy Trustee outside the CVM

All of this runs on `TRUSTEE_HOST`, never inside the CVM.

### 2.1 Provision the Trustee host

Create a plain Azure VM in the same virtual network as the CVM. It does not need a confidential SKU — it is the relying party, not the workload.

```bash
# RG and LOCATION are still set from Part 1
CVM_VNET=$(az network vnet list -g "$RG" --query "[0].name" -o tsv)
CVM_SUBNET=$(az network vnet subnet list -g "$RG" --vnet-name "$CVM_VNET" --query "[0].name" -o tsv)

az vm create \
  --resource-group "$RG" \
  --name propeller-trustee-host \
  --size Standard_D2als_v7 \
  --image Ubuntu2404 \
  --admin-username propeller \
  --ssh-key-values ~/.ssh/cloud.pub \
  --vnet-name "$CVM_VNET" \
  --subnet "$CVM_SUBNET" \
  --public-ip-sku Standard
```

Record both private and public IPs and allow the CVM to reach KBS.

`az vm show` does not return addresses; use `az vm list-ip-addresses`:

```bash
# Private IP
CVM_IP=$(az vm list-ip-addresses -g "$RG" -n propeller-amd-cvm \
  --query "[0].virtualMachine.network.privateIpAddresses[0]" -o tsv)
# Public IP
CVM_IP=$(az vm list-ip-addresses -g "$RG" -n propeller-amd-cvm \
  --query "[0].virtualMachine.network.publicIpAddresses[0].ipAddress" -o tsv)

# Private IP
TRUSTEE_IP=$(az vm list-ip-addresses -g "$RG" -n propeller-trustee-host \
  --query "[0].virtualMachine.network.privateIpAddresses[0]" -o tsv)
# Public IP
TRUSTEE_IP=$(az vm list-ip-addresses -g "$RG" -n propeller-trustee-host \
  --query "[0].virtualMachine.network.publicIpAddresses[0].ipAddress" -o tsv)

echo "TRUSTEE_HOST=$TRUSTEE_IP"
echo "CVM_HOST=$CVM_IP"
```

The equivalent through the NIC, if you prefer:

```bash
az network nic show -g "$RG" -n propeller-trustee-hostVMNic \
  --query "ipConfigurations[0].privateIPAddress" -o tsv
```

The NSG rule must allow the CVM's **private** IP to reach 8082:

```bash
# The Trustee host's NSG is named <vm-name>NSG by default.
# It already contains Azure's default-allow-ssh at priority 1000 in the Inbound
# direction, and priorities must be unique per direction — so use 1010.
az network nsg rule create \
  --resource-group "$RG" \
  --nsg-name propeller-trustee-hostNSG \
  --name allow-kbs-from-cvm \
  --priority 1010 \
  --direction Inbound \
  --access Allow \
  --protocol Tcp \
  --source-address-prefixes "$CVM_IP" \
  --destination-port-ranges 8082
```

A simpler alternative when both VMs already share a subnet and NSG: create the Trustee host with no public IP (`--public-ip-address ""`) and open the rule against the subnet prefix instead of the single CVM address.

Export the two addresses for the rest of the guide (still in the same shell):

```bash
export TRUSTEE_HOST=$TRUSTEE_IP
export CVM_HOST=$CVM_IP
```

Install Docker and Docker Compose on the Trustee host (the next steps run there):

```bash
ssh -i ~/.ssh/cloud propeller@$TRUSTEE_HOST
sudo apt update
sudo apt upgrade -y
sudo apt install -y git curl
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"   # log out and back in to take effect
sudo systemctl enable docker.service
```

### 2.2 Start the Trustee stack

KBS runs over **plain HTTP inside the vNet**. This is deliberate and is what the rest of this guide assumes.

The reason is a trust-store problem on the guest: `coco_keyprovider` links `reqwest` with `rustls-platform-verifier`, which on Linux loads roots through `rustls-native-certs`. Pointing it at a private CA via `SSL_CERT_FILE` did not work in testing — the handshake still failed with `UnknownIssuer` while `openssl s_client` against the same CA succeeded — so a self-signed KBS certificate cannot be trusted by the keyprovider. Since TLS here only protects a hop that never leaves the private subnet, and the image key is additionally wrapped under an RCAR-derived session key, HTTP is an acceptable trade for a working deployment. If you need TLS, terminate it at a component that reads the system store reliably, or issue the KBS certificate from a publicly trusted CA against a real DNS name.

Generate the KBS signing keys and start the stack:

```bash
git clone https://github.com/confidential-containers/trustee
cd trustee

openssl genpkey -algorithm ed25519 > kbs/config/private.key
openssl pkey -in kbs/config/private.key -pubout -out kbs/config/public.pub
```

Set KBS to HTTP and publish it on `8082`. The compose file publishes `8080` by default; this guide uses `8082`. Check the `kbs` service mapping:

```bash
awk '/^  kbs:/,/^  [a-z]/' docker-compose.yml | grep -A3 ports
```

If it publishes `8080:8080`, change the host side to `8082:8080`.

Edit `kbs/config/docker-compose/kbs-config.toml` so `[http_server]` is:

```toml
[http_server]
sockets = ["0.0.0.0:8080"]
insecure_http = true
private_key = "/opt/confidential-containers/kbs/user-keys/tls-key.pem"
certificate = "/opt/confidential-containers/kbs/user-keys/tls-cert.pem"
tls_profile = "intermediate"
```

`insecure_http = true` makes KBS ignore the key/cert paths above; they can stay as they are. `sockets` is the in-container bind address and stays `0.0.0.0:8080` — the host port comes from the compose mapping.

Ensure the `kbs` service maps host `8082` to container `8080`:

```yaml
kbs:
  ports:
    - "8082:8080"
```

Start the stack:

```bash
docker compose up -d
docker compose ps
```

### 2.3 Point KBS at the Attestation Service token CA

This step is easy to miss and produces a confusing failure. Trustee's `setup.sh` generates two independent PKI hierarchies in the same directory:

| File                                                  | Purpose                                                     |
| ----------------------------------------------------- | ----------------------------------------------------------- |
| `ca-cert.pem`, `ca.key`                               | **token-signing** CA (`CN=KBS-compose-root`) used by the AS |
| `token.key`, `token-cert.pem`, `token-cert-chain.pem` | AS EAR-token signing key/cert                               |
| `tls-*.pem`                                           | TLS endpoints, unrelated to attestation                     |

KBS verifies the AS-issued token against `[attestation_token].trusted_certs_paths`. It must point at the **token CA**, not the TLS CA. If it points at the wrong CA the keyprovider fails with:

```text
TokenVerifierError: Cannot verify token: neither trusted jwk set nor trusted pem public key works
```

```text
Failed to get KEK from KBS ... request unauthorized
```

Extract the token CA into its own file and point KBS at it:

```bash
cd ~/trustee/kbs/config/docker-compose

# the second certificate in the chain is the token-signing root
awk '/BEGIN CERTIFICATE/,/END CERTIFICATE/' token-cert-chain.pem \
  | awk 'BEGIN{n=0} /BEGIN CERTIFICATE/{n++} n==2' > token-ca-cert.pem

# sanity: the AS token cert must verify against it
openssl verify -CAfile token-ca-cert.pem token-cert.pem
# expect: token-cert.pem: OK

cd ~/trustee
sed -i 's|trusted_certs_paths = \[.*\]|trusted_certs_paths = ["/opt/confidential-containers/kbs/user-keys/token-ca-cert.pem"]|' \
  kbs/config/docker-compose/kbs-config.toml

grep -A2 '\[attestation_token\]' kbs/config/docker-compose/kbs-config.toml
docker compose up -d --force-recreate kbs
```

### 2.4 Verify KBS is reachable

Always use the **private IP**, never `0.0.0.0` — that is a bind address, not a connect address:

```bash
curl -s -o /dev/null -w 'http=%{http_code}\n' http://${TRUSTEE_HOST}:8082/kbs/v0/resource-policy
# expect http=401 — a JSON auth error, not a connection error
```

### 2.5 Create and upload the image key

`kbs-client` is built from the Trustee tree. On Ubuntu the build pulls in the Intel SGX DCAP verifier via `intel-tee-quote-verification-sys`, whose build script needs the **DCAP dev headers**. Without them the build fails with:

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

Once the flow works, replace this with a policy that requires a real `az-snp-vtpm` token. The default AS attestation policy (`default_cpu`) understands Azure vTPM evidence; if the KBS denies a request from the CVM, check the KBS resource policy and the AS attestation policy together.

## Part 3 — Guest stack inside the CVM

Everything in this part runs inside the confidential VM:

```bash
ssh -i ~/.ssh/cloud <admin-user>@$CVM_HOST   # admin user is propeller (Part 1)
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
  clang cmake jq unzip libtss2-dev tpm2-tools

curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
. "$HOME/.cargo/env"

WASMTIME_VERSION=v48.0.2
curl -L "https://github.com/bytecodealliance/wasmtime/releases/download/${WASMTIME_VERSION}/wasmtime-${WASMTIME_VERSION}-x86_64-linux.tar.xz" -o /tmp/wasmtime.tar.xz
tar -xf /tmp/wasmtime.tar.xz -C /tmp
sudo mv /tmp/wasmtime-${WASMTIME_VERSION}-x86_64-linux/wasmtime /usr/local/bin/
rm -rf /tmp/wasmtime*
wasmtime --version
```

### 3.2 Build Attestation Agent and CoCo Keyprovider

Build the OS image with the **`az-snp-vtpm`** attester. Do not use `all-attesters`: that enables the NVIDIA attester, whose build script needs the NVIDIA C++ attestation SDK and fails with:

```text
Header file not found at ".../nv-attestation-sdk-cpp/build/include/nvat.h"
If you're building from source, please build the C++ SDK first.
```

Two features matter, and they are easy to get wrong:

- **`az-snp-vtpm-attester`** is the Azure vTPM attester. Without it the guest falls back to the generic TPM attester, which looks for an AK at `0x81010002`. Azure's vTPM does not have that handle and evidence generation fails.
- **`tpm-attester` must not be combined with the Azure attesters.** If it is, `detect_attestable_devices()` adds a generic TPM as an _additional_ device, and composite evidence then demands the same missing `0x81010002` quote. This is the bug fixed upstream by excluding `az-*-vtpm-attester` from that path; a branch that predates the fix will hit it.

Clone the fork and check out the rebased branch:

```bash
cd /tmp
git clone https://github.com/rodneyosodo/guest-components.git
cd guest-components
git checkout upstream-proplet-rebased   # or whatever branch you built

# Attestation Agent (gRPC) with the Azure attester only
cd attestation-agent
make ATTESTER=az-snp-vtpm-attester ttrpc=false
sudo make install

# CoCo Keyprovider — select the Azure attester; do NOT use default (all-attesters)
cd coco_keyprovider
cargo build --release --target x86_64-unknown-linux-gnu \
  --no-default-features --features az-snp-vtpm-attester
sudo cp ../../target/x86_64-unknown-linux-gnu/release/coco_keyprovider /usr/local/bin/

attestation-agent --help >/dev/null && echo "AA OK"
coco_keyprovider --help >/dev/null && echo "keyprovider OK"
```

If the keyprovider's `Cargo.toml` does not expose an `az-snp-vtpm-attester` feature, add it (and make `kbs_protocol` non-default) so the attester set is selectable at build time:

```toml
kbs_protocol = { path = "../kbs_protocol", default-features = false, features = [
    "background_check",
    "rust-crypto",
] }

[features]
default = ["all-attesters"]
all-attesters = ["kbs_protocol/all-attesters"]
az-snp-vtpm-attester = ["kbs_protocol/az-snp-vtpm-attester"]
```

### 3.3 Build Proplet with the Azure attester enabled

Then build:

```bash
git clone https://github.com/absmach/propeller.git ~/propeller
cd ~/propeller/proplet
cargo build --release
sudo cp target/release/proplet /usr/local/bin/
```

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

A successful proplet start logs `TEE runtime initialized successfully`. When a task runs, the keyprovider logs an RCAR handshake and the AS logs `Verifier/endorsement check passed. tee=AzSnpVtpm`.

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

`skopeo copy` pushes directly to the registry — **do not** follow it with `docker push docker://...`. `docker push` does not accept the `docker://` scheme and fails with `invalid reference format`:

`Copying blob ... skipped: already exists` during the copy is normal — the plaintext layer is already in the registry from the `wasm-to-oci push`, and only the encrypted layer needs uploading. If `skopeo copy` prints `Writing manifest to image destination`, the push succeeded.

## Part 5 — Run an encrypted workload and verify

Submit a task. Two fields are easy to get wrong:

- **`image_url` must be a bare OCI reference** — no `docker://` scheme. That prefix belongs to `skopeo`/`wasm-to-oci` commands, not to task definitions. Passing it produces `Failed to parse image reference: invalid reference format` before any attestation happens.
- **Do not include a `file` field** for encrypted workloads.

```json
{
  "name": "add",
  "image_url": "rodneydav/azure-tee-wasm-addition:encrypted",
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
    "image_url": "rodneydav/azure-tee-wasm-addition:encrypted",
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

`state: 3` with `results: '30\n'` is success — that is `10 + 20`, decrypted and executed inside the CVM.

A successful run means the whole chain worked: the AA produced vTPM evidence, the AS verified it (`Verifier/endorsement check passed. tee=AzSnpVtpm`), KBS released the key, CoCo Keyprovider unwrapped it, and `image-rs` decrypted the layer inside the CVM.

### Reading failures

| Error                               | Stage              | Meaning                                                                                           |
| ----------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------- |
| `Failed to parse image reference`   | task validation    | `docker://` in `image_url`                                                                        |
| `Failed to pull and decrypt layers` | image pull         | see the keyprovider log for the real cause                                                        |
| `Failed to get KEK from KBS`        | key release        | KBS refused; check the keyprovider log for `UnknownIssuer`, `PolicyDeny`, or `TokenVerifierError` |
| `TokenVerifierError`                | token verification | KBS is trusting the wrong CA — see [2.3](#23-point-kbs-at-the-attestation-service-token-ca)       |
| `get composite evidence failed`     | attestation        | generic TPM attester selected — see [3.2](#32-build-attestation-agent-and-coco-keyprovider)       |

The keyprovider log is where the real error lives; proplet's message is a summary:

```bash
sudo journalctl -u coco-keyprovider --since "2 min ago" --no-pager | grep -vi tss_esapi | tail -20
```

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
platform-info: type=AmdSev version=0.1.0
list-capabilities: 5 entries
random(32): <64 hex chars>
system-time: <seconds>s <nanoseconds>ns
sha256(hello)=2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
generate-keypair: ok (pub=<n>B priv=<n>B)
```

```bash
./build/hal-runner ./build/attestation-test.wasm --function run-attestation
```

Expected, with a wasmhal build that includes the fix:

```text
platform-info: type=AmdSev version=0.1.0
attestation: ok (evidence len=12316)
evidence: 7b2276657273696f6e223a312c2274706d5f71756f7465223a...
```

## References

- [Azure Confidential VMs guide](https://propeller.absmach.eu/docs/azure-cvm)
- [Trustee](https://github.com/confidential-containers/trustee)
- [Guest Components](https://github.com/confidential-containers/guest-components)
- [Propeller encrypted workloads guide](https://propeller.absmach.eu/docs/tee)
- [Azure confidential VM guest attestation](https://github.com/Azure/confidential-computing-cvm-guest-attestation)
