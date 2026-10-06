# Propeller on an Intel TDX GCP Confidential VM with Trustee

For the rendered version of this guide, see [propeller.absmach.eu/docs/gcp-cvm](https://propeller.absmach.eu/docs/gcp-cvm).

Deploy [Trustee](https://github.com/confidential-containers/trustee) **outside** the confidential VM and run the Propeller stack with the CoCo **guest
components** **inside** a GCP Intel TDX confidential VM. This is the production-shaped topology: the relying party (Trustee) verifies evidence and releases keys, and it must not share the trust boundary with the workload it is attesting.

This is the Intel TDX counterpart of [`../azure-sev/README.md`](../azure-sev/README.md), which covers AMD SEV-SNP on Azure. There is also [`../gcp-sev/README.md`](../gcp-sev/README.md) for AMD SEV-SNP on GCP — same provider, different TEE, and not the same as the Azure guide because GCP exposes the firmware device directly instead of a vTPM. Everything in Parts 4–6 (publishing an encrypted image, running a workload, exercising the HAL) is identical across all three; Parts 1–3 are where the platforms differ, and those differences are called out as they come up.

## Architecture

| Component                               | Runs on                | Role                                                                 |
| --------------------------------------- | ---------------------- | -------------------------------------------------------------------- |
| KBS (Key Broker Service)                | Trustee host (outside) | Validates the attestation token, releases decryption keys            |
| AS (Attestation Service)                | Trustee host (outside) | Verifies TEE evidence (here: `tdx`)                                  |
| RVPS (Reference Value Provider Service) | Trustee host (outside) | Reference values for verification                                    |
| Attestation Agent (AA)                  | CVM (inside)           | Collects a TDX quote from the Linux TSM, performs the RCAR handshake |
| CoCo Keyprovider                        | CVM (inside)           | Image key-wrap/unwrap keyprovider for `image-rs`                     |
| Proplet                                 | CVM (inside)           | Pulls and runs the encrypted workload                                |
| Wasmtime                                | CVM (inside)           | Executes the decrypted WASM                                          |

### Where to run Trustee

Trustee runs on a **separate GCE VM you control** — the _Trustee host_ — never inside the CVM and never on the Google host. The whole point of a CVM is that the machine hosting it (and its operator) is untrusted, so the component that verifies evidence and releases decryption keys must be its own trust domain. Put the Trustee host in the same VPC network as the CVM and have the CVM reach KBS over the private network; do not expose KBS publicly.

| Placement                                                 | Correct?                                   |
| --------------------------------------------------------- | ------------------------------------------ |
| A separate GCE VM in the same VPC (internal IP)           | Yes                                        |
| Any machine or server you control, reachable from the CVM | Yes                                        |
| Inside the CVM                                            | No — the keys would live with the workload |
| The CVM's Google host / hypervisor                        | No — you do not control it                 |

Provisioning is in [Part 2](#part-2--deploy-trustee-outside-the-cvm).

## What is different from the Azure AMD SEV-SNP guide

Intel TDX on GCP differs from AMD SEV-SNP on Azure in ways that matter while building the guest stack:

|                          | Azure AMD SEV-SNP CVM                               | GCP Intel TDX CVM                                                            |
| ------------------------ | --------------------------------------------------- | ---------------------------------------------------------------------------- |
| Attester                 | `az-snp-vtpm-attester`                              | `tdx-attester`                                                               |
| Evidence source          | Guest vTPM (HCL report + TPM quote)                 | Intel DCAP quote from the Linux TSM (`/dev/tdx_guest`)                       |
| Enable the platform      | `--security-type ConfidentialVM --enable-vtpm true` | `--confidential-compute-type=TDX`                                            |
| Machine family           | `DCasv5` / `ECasv5`                                 | `c3-standard-*` (`c4-standard-*` in preview)                                 |
| Image                    | `Canonical:…server-cvm` (a CVM-specific image)      | Any `TDX_CAPABLE` image, e.g. Ubuntu Pro `ubuntu-pro-2604-resolute-amd64`    |
| Secure Boot / vTPM flags | Required                                            | Optional (`--shielded-*`, as the console sets); attestation ignores the vTPM |
| Boot disk                | Managed by the SKU                                  | `pd-balanced` only (NVMe interface)                                          |
| DCAP dev headers         | Needed for the keyprovider build                    | Not needed inside the guest — `tdx-attester` talks to the TSM directly       |
| Boot chain measured into | PCRs via the vTPM                                   | RTMRs via the TDX quote; GCP boots GRUB, not the TDVF shim                   |
| Guest verification       | `Detected confidential virtualization sev-snp`      | `Memory Encryption Features active: TDX`                                     |

Two of these are easy to get wrong:

- **Do not carry the Azure flags over.** `--enable-vtpm` and the CVM image family do not exist here, and TDX evidence does not come from a vTPM. A TDX CVM with no `/dev/tdx_guest` cannot produce evidence at all.
- **A `/dev/tpm0` on the guest is not the attestation path.** `--shielded-vtpm` gives this VM a vTPM, and the TDX attester ignores it. The trap is building the AA with `tpm-attester` as well: see [1.3](#13-confirm-the-guest-is-really-in-a-td).
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
  --project=$PROJECT \
  --zone=$ZONE \
  --machine-type=c3-standard-4 \
  --network-interface=network-tier=PREMIUM,nic-type=GVNIC,stack-type=IPV4_ONLY,subnet=default \
  --maintenance-policy=TERMINATE \
  --provisioning-model=STANDARD \
  --tags=propeller,http-server,https-server \
  --create-disk=auto-delete=yes,boot=yes,mode=rw,size=40,type=pd-balanced,image=projects/ubuntu-os-pro-cloud/global/images/ubuntu-pro-2604-resolute-amd64-v20260918 \
  --shielded-secure-boot \
  --shielded-vtpm \
  --shielded-integrity-monitoring \
  --confidential-compute-type=TDX
```

The image goes **inside** `--create-disk` here, not in a separate `--image` flag. `--image` (and `--image-family`) and `--create-disk` each describe a boot disk, so passing both asks for two and `gcloud` refuses:

```text
ERROR: (gcloud.compute.instances.create) Invalid value for [--disk]: Each instance can have
exactly one boot disk. One boot disk was specified through [--disk or --create-disk] and
another through [--image].
```

If you would rather keep the image as its own flag, drop `--create-disk` and set the disk with `--boot-disk-size`, `--boot-disk-type=pd-balanced` and `--boot-disk-auto-delete` instead — just not both forms at once.

These flags are what make the CVM work:

| Flag                                      | Why it is there                                                                                                                                                                     |
| ----------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--machine-type=c3-standard-*`            | Intel TDX is only offered on the C3 (Sapphire Rapids) and C4 (Granite Rapids, preview) families. A non-C3/C4 machine type fails at creation.                                        |
| `--confidential-compute-type=TDX`         | This is what puts the VM in a TD. Without it the same machine type produces a regular VM with no evidence and no `/dev/tdx_guest`.                                                  |
| `--maintenance-policy=TERMINATE`          | Live migration is not supported for TDX instances; `gcloud` rejects the create without it.                                                                                          |
| `--create-disk=…type=pd-balanced,image=…` | TDX supports only Balanced Persistent Disk on the NVMe interface, so `pd-standard` is not an option. The image belongs in this flag too — see the note below it.                    |
| `image=…` (in `--create-disk`)            | Must be a `TDX_CAPABLE` image. Any of the Ubuntu Pro or `ubuntu-os-cloud` families work; what matters is the `TDX_CAPABLE` feature and a kernel with the TDX guest driver.          |
| `--shielded-secure-boot`                  | Optional for TDX, and it is what the console adds. It measures the boot chain, which on TDX lands in the RTMRs.                                                                     |
| `--shielded-vtpm`                         | Optional, and **not** what attestation uses — see [1.3](#13-confirm-the-guest-is-really-in-a-td). The console adds it for every Shielded VM.                                        |
| `--shielded-integrity-monitoring`         | Optional. Useful: it is what makes the boot-chain measurements in the quote worth pinning.                                                                                          |
| `--tags=propeller,…`                      | The `propeller` tag is what the KBS firewall rule in [2.1](#21-provision-the-trustee-host) matches on. `http-server,https-server` are the console defaults and are not needed here. |
| `--network-interface=…`                   | Premium tier, gVNIC, IPv4 only — the defaults for a confidential instance. Named explicitly so the shape matches what the console produces.                                         |

Things the console-generated command also carries, none of which affect attestation: `--service-account` with the default compute SA, the `--scopes` list, `--labels=goog-ec-src=vm_add-gcloud`, and `--reservation-affinity=any`. Keep them if you like; the guide does not depend on them.

Two naming details if you created the instance from the console instead: it is called `instance-<timestamp>` rather than `propeller-intel-tdx-cvm`, and `--create-disk` carries `device-name=instance-<timestamp>`. Either name works as long as you use it consistently below — every later command in this guide refers to `propeller-intel-tdx-cvm`.

### 1.1 Pick a zone that has TDX

TDX is offered in a subset of zones, and the list moves over time — the authoritative table is [Supported configurations](https://cloud.google.com/confidential-computing/confidential-vm/docs/supported-configurations). `us-central1-a` supports `c3-standard-*`; plenty of regions do not.

To check from the CLI that a candidate zone offers the machine type at all:

```bash
gcloud compute machine-types list \
  --filter="name:c3-standard-4 AND zone:~$ZONE" \
  --format="value(name)"
```

Being offered in the zone is necessary but not sufficient — a plain (non-TDX) `c3-standard-4` is the same machine type, and the zone table above is the only authoritative answer. If the create in Part 1 fails with a confidential-compute error, that is the reason.

Confirm the image you are about to use supports TDX isolation:

```bash
gcloud compute images describe ubuntu-pro-2604-resolute-amd64-v20260918 \
  --project ubuntu-os-pro-cloud \
  --format="value(guestOsFeatures)"
# expect: type: TDX_CAPABLE  (among others)
```

To see every image that qualifies:

```bash
gcloud compute images list \
  --filter="guestOsFeatures[].type:(TDX_CAPABLE)" \
  --format="table(name,family,project)"
```

An image without `TDX_CAPABLE` may still be created, but its kernel can lack the `tdx_guest` module — which is the failure in [1.3](#13-confirm-the-guest-is-really-in-a-td).

### 1.2 Connect and record the addresses

A stock GCP image has no `propeller` user, so create one and install your key — the rest of the guide assumes you SSH as `propeller`:

```bash
CVM_IP=$(gcloud compute instances describe propeller-intel-tdx-cvm \
  --zone "$ZONE" --format='get(networkInterfaces[0].accessConfigs[0].natIP)')

# from your own machine or an existing admin session on the instance
sudo adduser --disabled-password --gecos "" propeller
sudo adduser propeller sudo
sudo install -d -m 700 -o propeller -g propeller /home/propeller/.ssh
sudo tee -a /home/propeller/.ssh/authorized_keys < ~/.ssh/cloud.pub
sudo chown propeller:propeller /home/propeller/.ssh/authorized_keys

# passwordless sudo — required, see below
sudo chown propeller:propeller /home/propeller/.ssh/authorized_keys
sudo chmod 0440 /etc/sudoers.d/90-propeller
```

The sudoers line is not optional. `--disabled-password` gives the account an **empty** password, and `sudo` will not accept an empty one — so without that line every `sudo` in this guide re-prompts and then fails:

```text
[sudo] password for propeller:
sudo: 1 incorrect password attempt
```

This is exactly how the stock images behave for their own login user, which is why `gcloud compute ssh` needs no password: the guest agent installs a sudoers drop-in. Recreating that one line is all it takes.

`gcloud compute ssh propeller-intel-tdx-cvm --zone "$ZONE"` is the shortcut that skips the whole block: it creates the user, installs the key, and sets up passwordless sudo — under your Google account name, so use that name in place of `propeller` for the rest of the guide if you take this route.

Use the instance's **external** IP for SSH, and its **internal** IP for everything the guest dials (KBS in Part 3). Keeping those two separate is the whole point of the split in the architecture table above.

Already stuck at a `sudo: authenticate` prompt? Press Ctrl-C. `sudo` has no way to authenticate an empty password, so retrying cannot help; you need root by another route — either delete the instance and re-create it with the block above, or start a fresh shell as a user that already has sudo (your Google account via OS Login, or the `gcloud compute ssh` account) and add the sudoers line from there.

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

`/dev/tdx_guest` and the TSM are the two things that must be there. Everything else on this guest can be absent or present without affecting attestation.

If `Memory Encryption Features active: TDX` is missing, the VM is not confidential: delete it and re-create with `--confidential-compute-type=TDX`.

If `/dev/tdx_guest` is missing while TDX is active, the guest kernel lacks the driver module:

```bash
sudo modprobe tdx_guest
ls -l /dev/tdx_guest
```

If the module does not exist, the image is not `TDX_CAPABLE` — use a different image family.

Expect a `/dev/tpm0` on this VM, because `--shielded-vtpm` was requested, and ignore it. That vTPM is a Shielded VM feature, not the attestation path on TDX: the evidence is a quote from `/dev/tdx_guest`, and a vTPM sitting next to it is a trap rather than a help. Two consequences:

- Do **not** build the AA or the keyprovider with `tpm-attester`. A generic TPM attester makes `detect_attestable_devices()` report the TPM as an extra device, and composite evidence then demands a TPM quote that says nothing about the TD.
- Nothing else in this stack reads `/dev/tpm0`. If a log ever shows the Azure `az-snp-vtpm` attester being selected on a TDX guest, that is the bug this note is about.

`/dev/sev*` is absent, which is correct on an Intel machine.

## Part 2 — Deploy Trustee outside the CVM

All of this runs on `TRUSTEE_HOST`, never inside the CVM.

### 2.1 Provision the Trustee host

Create a plain GCE VM in the same VPC as the CVM. It does not need a confidential SKU — it is the relying party, not the workload.

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

Note the absence of `--maintenance-policy=TERMINATE`, which the CVM in Part 1 needs and this VM must not have. Live migration is unsupported for confidential instances, so a CVM has to terminate on host maintenance; a plain `e2` instance can be live-migrated, and asking it to terminate is rejected:

```text
ERROR: (gcloud.compute.instances.create) Could not fetch resource:
 - e2 instances do not support onHostMaintenance=TERMINATE unless they are preemptible.
```

`TERMINATE` on a non-confidential VM is only legal if the instance is preemptible/spot, which is not what you want for the host holding your keys.

This one uses the `--boot-disk-*` form rather than `--create-disk`, which is the same choice described in Part 1: pick one or the other, never both with `--image`.

Unlike a CVM this one needs no confidential flags, and it does not need the same OS as the CVM. 24.04 LTS is deliberate: [2.5](#25-create-and-upload-the-image-key) installs Intel's DCAP headers from Intel's own apt repository, and that repository is the moving part — it is known to cover 22.04 and 24.04, so the newest LTS you can build `kbs-client` on is the safest host.

`--metadata=enable-oslogin=false` is not set here because OS Login is off by default on GCP; if your project metadata turns it on, add `--metadata=enable-oslogin=false` or drop the manual user creation in [1.2](#12-connect-and-record-the-addresses) and SSH as your Google account name instead.

Record both addresses — the guest dials the **internal** one, you SSH to the **external** one:

```bash
TRUSTEE_INTERNAL=$(gcloud compute instances describe propeller-trustee-host \
  --zone "$ZONE" --format='get(networkInterfaces[0].networkIP)')
TRUSTEE_EXTERNAL=$(gcloud compute instances describe propeller-trustee-host \
  --zone "$ZONE" --format='get(networkInterfaces[0].accessConfigs[0].natIP)')

echo "TRUSTEE_HOST=$TRUSTEE_INTERNAL   # what the guest dials"
echo "TRUSTEE_SSH=$TRUSTEE_EXTERNAL"
```

In a default VPC network the CVM can already reach `8082`: the built-in `default-allow-internal` rule permits all TCP from `10.128.0.0/9`, and both instances sit in that range. Nothing more is needed for the flow below.

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

| File                                                  | Purpose                                                     |
| ----------------------------------------------------- | ----------------------------------------------------------- |
| `ca-cert.pem`, `ca.key`                               | **token-signing** CA (`CN=KBS-compose-root`) used by the AS |
| `token.key`, `token-cert.pem`, `token-cert-chain.pem` | AS EAR-token signing key/cert                               |
| `tls-*.pem`                                           | TLS endpoints, unrelated to attestation                     |

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

Install Intel's package repository **first**, then the toolchain. The two `libsgx-dcap-*` packages live in Intel's repo, not in Ubuntu, so an install that includes them fails on a clean VM until the repo exists:

```bash
# 1. Intel's SGX/DCAP repository — provides libsgx-dcap-quote-verify-dev and
#    libsgx-dcap-default-qpl. $(lsb_release -cs) is the codename (noble on 24.04).
curl -fsSL https://download.01.org/intel-sgx/sgx_repo/ubuntu/intel-sgx-deb.key \
  | sudo gpg --dearmor -o /usr/share/keyrings/intel-sgx.gpg
echo "deb [arch=amd64 signed-by=/usr/share/keyrings/intel-sgx.gpg] https://download.01.org/intel-sgx/sgx_repo/ubuntu $(lsb_release -cs) main" \
  | sudo tee /etc/apt/sources.list.d/intel-sgx.list

# 2. Now the packages resolve: the toolchain and TSS headers from Ubuntu,
#    the DCAP headers from Intel's repo.
sudo apt-get update
sudo apt-get install -y \
  build-essential gcc make pkg-config libssl-dev openssl curl git \
  clang cmake jq unzip libtss2-dev tpm2-tools \
  libclang-dev libsgx-dcap-quote-verify-dev libsgx-dcap-default-qpl \
  protobuf-compiler

curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
. "$HOME/.cargo/env"
```

Doing it the other way round produces exactly this, and the rest of the packages still install:

```text
E: Unable to locate package libsgx-dcap-quote-verify-dev
E: Unable to locate package libsgx-dcap-default-qpl
```

If Intel's repository does not carry your codename — it lags new Ubuntu releases — `apt-get update` prints a 404 for that suite and the packages stay missing. Check what it published:

```bash
curl -s https://download.01.org/intel-sgx/sgx_repo/ubuntu/dists/ | grep -o 'href="[^"]*"'
```

Both `jammy` (22.04) and `noble` (24.04) carry `libsgx-dcap-quote-verify-dev` and `libsgx-dcap-default-qpl`, which is why the Trustee host in [2.1](#21-provision-the-trustee-host) stays on 24.04 LTS. If your codename is missing, move the Trustee host to a release that is covered rather than hand-pinning a `.deb` URL, which then has to be redone on every upgrade.

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
git checkout upstream-proplet-rebased

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

**Check out `upstream-proplet-rebased`, not `enable-wasm-workloads`.** The two branches are not the same tree, and on the wrong one the keyprovider is encrypt-only: its `Cargo.toml` has no features at all and its `un_wrap_key` returns `Status::unimplemented`. That produces either of these, both of which mean "wrong branch":

```text
error: the package 'coco_keyprovider' does not contain this feature: tdx-attester
help: packages with the missing feature: attestation-agent, attester, kbs_protocol, kbc
```

```text
UnWrapKey API is unimplemented!
```

The second one is worse, because the binary builds and installs cleanly and only fails later, at decrypt time. `un_wrap_key` is the path proplet's `image-rs` uses through `ocicrypt-rs/keywrap-keyprovider-grpc`, so an unimplemented one means no workload can ever decrypt a layer no matter what the attestation does.

Note that proplet's own Cargo dependencies point at `enable-wasm-workloads` for the guest-components _libraries_ (`kbs_protocol`, `attestation-agent`, `image-rs`), while the binaries you build here come from `upstream-proplet-rebased`. That split is inherited from the Azure guide and it works, but it is the first place to look if the AA binary and proplet ever disagree about a config key or an evidence format.

Two more things are easy to get wrong in the commands above:

- **Do not use `all-attesters`.** That enables the NVIDIA attester, whose build script needs the NVIDIA C++ attestation SDK and fails with `Header file not found at ".../nv-attestation-sdk-cpp/build/include/nvat.h"`. Passing `ATTESTER=tdx-attester` to `make` replaces `all-attesters` rather than adding to it, and the keyprovider's own default has to be overridden with `--no-default-features`.
- **`tdx-attester` must not be combined with `tpm-attester`.** With a generic TPM attester compiled in, `detect_attestable_devices()` adds a TPM as an _additional_ device and composite evidence then demands a quote for a TPM that is not bound to the TD. Do not add it — see [1.3](#13-confirm-the-guest-is-really-in-a-td) for why this VM's `/dev/tpm0` makes that tempting.

If you must build the keyprovider from a branch whose `Cargo.toml` has an empty `[features]`, add the dependency and the feature forwarding yourself:

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

That only makes the build work. The `un_wrap_key` gap on such a branch is a source-level omission and no manifest edit closes it.

### 3.3 Build Proplet

```bash
git clone https://github.com/absmach/propeller.git ~/propeller
cd ~/propeller/proplet
cargo build --release
sudo cp target/release/proplet /usr/local/bin/
```

No feature flags: one proplet binary serves both platforms. `proplet/Cargo.toml` compiles the `tdx-attester` and the `az-snp-vtpm-attester` together, and both HAL platforms, and the running platform decides which one is primary — `detect_tee_type()` probes TDX first and falls through to the Azure vTPM.

Both attesters are compiled rather than selected by a cargo feature on purpose. A feature would be the obvious way to express "build for one platform", and it is what the first version of this guide did, but features are not target-scoped: enabling `attestation-agent/az-snp-vtpm-attester` from `[features]` pulls `tss-esapi-sys` into the arm64 and riscv64 builds, which CI builds and which fail without the TPM2 TSS headers. Keeping the choice in the `[target.'cfg(target_arch = "x86_64")'.dependencies]` block is what leaves those builds alone.

This is a property of the tree, not of the build command, so there is nothing to pass on the command line. Confirm it is there before building:

```bash
cd ~/propeller/proplet
grep -c tdx-attester Cargo.toml     # expect 2
```

If it prints `0`, your clone predates the TDX support and a build from it will start fine on a TDX guest while quietly using the sample attester ([3.7](#37-what-a-healthy-proplet-start-looks-like)). Pull a tree that contains it rather than reinstating the workaround; the attester has to be compiled in, and no environment variable adds it.

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
PROPLET_HAL_ENABLED=true
PROPLET_KBS_URI=http://${TRUSTEE_HOST}:8082
PROPLET_AA_CONFIG_PATH=/etc/attestation-agent.toml
PROPLET_LAYER_STORE_PATH=/tmp/proplet/layers
EOF
```

**`PROPLET_HAL_ENABLED=true` and `PROPLET_EXTERNAL_WASM_RUNTIME` are mutually exclusive.** The external runtime is a separate `wasmtime` process that proplet drives over a socket; the HAL is an in-process host implementation, so setting both makes proplet log this at startup and then carry on with the HAL switched off:

```text
WARN PROPLET_HAL_ENABLED is set but the external Wasm runtime does not support the HAL; ignoring it
```

Pick the one your workload needs:

| Workload                                                                         | Setting                                                                                                             |
| -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| Plain WASM tasks, or anything using Wasmtime features the external runtime lacks | `PROPLET_EXTERNAL_WASM_RUNTIME=/usr/local/bin/wasmtime`, and leave `PROPLET_HAL_ENABLED` unset                      |
| A WASM component calling `elastic:hal/*`                                         | `PROPLET_HAL_ENABLED=true`, and **do not** set `PROPLET_EXTERNAL_WASM_RUNTIME` — proplet uses its embedded Wasmtime |

The guide's template assumes the second row, because [Part 6](#part-6--test-the-hal-interfaces-directly) and any HAL-bearing workload need the in-process host. If you only run plain WASM tasks, swap the two: set the external runtime and delete the `PROPLET_HAL_ENABLED` line.

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
sudo systemctl status attestation-agent coco-keyprovider proplet --no-pager
```

The `coco-keyprovider` unit above is the only one written with an **unquoted** heredoc (`<<EOF`), because it is the only one that needs `$TRUSTEE_HOST` expanded into it. The other two units use `<<'EOF'` and contain no variables. Check the address landed before moving on:

```bash
systemctl cat coco-keyprovider | grep ExecStart
# expect: ExecStart=/usr/local/bin/coco_keyprovider --socket 127.0.0.1:50011 --kbs http://10.x.x.x:8082
```

A literal `$TRUSTEE_HOST` or `<TRUSTEE_HOST>` here is not a cosmetic problem: `un_wrap_key` reads that argument and nothing else, so the keyprovider starts, accepts the gRPC call, and fails every decryption with `KBS URL not configured` — see [Reading failures](#reading-failures).

### 3.6 Watch the services

```bash
sudo journalctl -u attestation-agent -f
sudo journalctl -u coco-keyprovider -f
sudo journalctl -u proplet -f
```

A successful proplet start logs `TEE runtime initialized successfully`. When a task runs, the keyprovider logs an RCAR handshake and the AS logs an endorsement check for the TDX verifier.

Note `proplet.service` has `Requires=coco-keyprovider.service`, so stopping the keyprovider also stops proplet — restart both together.

### 3.7 What a healthy proplet start looks like

A TDX guest produces these lines, in this order:

```text
ELASTIC TEE HAL initialized for platform: IntelTdx
  - TDX guest device: /dev/tdx_guest
  - TSM support: available
Detected system limits: 13811MB RAM, 4 CPU cores (TEE: TDX=true, SEV=false)
TEE runtime initialized successfully
```

`Detected system limits: … (TEE: TDX=true, SEV=false)` is proplet's own platform detection, and it is independent of the attester: seeing `TDX=true` here does **not** mean the attester is right. Check that instead with a filtered restart:

```bash
sudo systemctl restart proplet
sudo journalctl -u proplet --since "1 min ago" | grep -E "TEE|TPM|Attester"
```

On a correct build the output is just `ELASTIC TEE HAL initialized for platform: IntelTdx` and `TEE runtime initialized successfully` — nothing about TPMs or an attester. If you also see this, the binary was built without `tdx-attester` ([3.3](#33-build-proplet)) and every evidence request it makes locally falls back to the sample attester:

```text
WARNING:esys:src/tss2-esys/api/Esys_NV_ReadPublic.c:309:Esys_NV_ReadPublic_Finish() Received TPM Error
ERROR:esys:src/tss2-esys/esys_tr.c:243:Esys_TR_FromTPMPublic_Finish() Error NV_ReadPublic ErrorCode (0x0000018b)
WARN No TEE platform detected. Sample Attester will be used.
```

The `esys` errors are the cause, not a separate problem: with only `az-snp-vtpm-attester` compiled in, proplet asks the **GCP** vTPM for Azure HCL data, gets nothing back, and falls through every attester to the sample one. `/dev/tpm0` exists on this VM because `--shielded-vtpm` was requested, which is what makes the Azure attester try at all.

This does not break the encrypted-workload path on its own. proplet pulls and decrypts through `image-rs` → the keyprovider gRPC socket on `127.0.0.1:50011` → the `coco-keyprovider` binary from [3.2](#32-build-attestation-agent-and-coco-keyprovider-with-the-tdx-attester), and the RCAR handshake runs there, against the AA daemon. The sample-attester fallback in proplet is only reached by `start_app` when the HAL attestation returns nothing. Rebuild proplet from a tree with `tdx-attester` anyway — see [3.3](#33-build-proplet) — because the fallback would collect meaningless evidence the moment the HAL path is not taken.

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
wasm-to-oci push build/addition.wasm docker.io/rodneydav/gcp-tee-wasm-addition:latest --server docker.io

# Encrypt it with the key stored in KBS
mkdir -p output
docker run \
  -v "$PWD/output:/output" \
  docker.io/rodneydav/coco-keyprovider:latest \
  /encrypt.sh \
  -k "$(cat ./private_key)" \
  -i kbs:///default/key/propeller-addition \
  -s docker://docker.io/rodneydav/gcp-tee-wasm-addition:latest \
  -d dir:/output

# Install skopeo
sudo apt install -y skopeo

# Push the encrypted image
skopeo login docker.io
skopeo copy dir:$(pwd)/output docker://rodneydav/gcp-tee-wasm-addition:encrypted
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
    "image_url": "rodneydav/gcp-tee-wasm-addition:encrypted",
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

### Tightening the attestation policy

`allow_all` was used above so that policy is not what blocks the first run. It releases the key to any guest that can reach KBS, which defeats the point of attesting.

To require a genuine TDX guest, you need two things: reference values for the fields Trustee's `default_cpu` policy checks, and a resource policy that inspects the resulting attestation token. Two details are specific to this platform:

- **GCP boots GRUB, not the TDVF shim.** Trustee's TDX policy has a branch for the shim/TDVF boot flow, which checks the UEFI event log for `File(kernel)` and `LOADED_IMAGE::LoadOptions`. A stock GCP VM does not produce those events, so evaluation takes the other branch, which compares `rtmr_1` and `rtmr_2` against reference values. Expect `rtmr_1` to change on every kernel or initrd update, and treat its reference value as something you re-collect and re-upload when the guest image changes.
- **The claims come from the quote, not from a vTPM**, so the reference values are TDX-specific: `mr_td` (the TD firmware measurement), `rtmr_1`, `rtmr_2`, and `xfam` (the enabled TD attributes). There is no `measurement` claim to pin the way an Azure vTPM guest has one.

Collect the values from a quote on your own guest first — the AS log or a `trustauthority-cli evidence --tdx` run both expose them — then upload them through the RVPS and write a resource policy that requires the TDX submod's trust vector. Treat this as the follow-up work after the end-to-end flow works, not as part of getting there.

## Part 6 — Test the HAL interfaces directly

This is independent of Trustee and only exercises the WASM-facing HAL providers. It is useful to confirm the non-TEE interfaces work inside the CVM.

**Run it in the CVM.** `hal-runner` embeds the HAL and detects the platform itself, so on any host without a TEE — the Trustee host included — it reports the stub and the attestation call fails by design:

```text
platform-info: type=None version=0.0.0
attestation: failed (no TEE platform available for attestation)
```

That is the correct answer for a plain VM, not a symptom to chase.

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
random(32): 1e892d47fbc8242cee4ba72c1f16a4d6db1883048aba16576ca1760132258fb8
system-time: 1791206962s 599790953ns
sha256(hello)=2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
generate-keypair: ok (pub=32B priv=32B)
```

```bash
./build/hal-runner ./build/attestation-test.wasm --function run-attestation
```

Expected:

```text
platform-info: type=IntelTdx version=0.1.0
attestation: ok (evidence len=625)
evidence: 7b226d6561737572656d656e7473223a7b2268616c223a2261616632323436616238373865303964633233653738363635366633663565643635383039376335646535396139666564386361303035316639636337303433222c226d727464223a22633165653963313665336166633530366366653034326335623834366133363835323866336233373631386561666232373436396263313134636639313465393232326339313631383437306537663262323861633336303936383237306135222c2272746d7230223a22343939663530303463623236303464366139623835386236663138376335386461663539323263363331376431356230323166613931396664623132666237366366386366666137633331313565653932383132643965316563373835666263222c2272746d7231223a22326637653537633863643464623433363336646161633337626662366236633932643736356437356133396534326461396136633238386533313235663634326538396335366134323362303763346430333737336664656261343539346362222c2272746d7232223a22393432333461633030386661303364626136623962303863346137666136623965313230366633653963346161303563393764303139653332396565353032663531626362333436613035613661663263316630376130616439323961633661222c2272746d7233223a22303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030227d7d
```

The HAL asks the Linux TSM for a DCAP quote, parses `MRTD` and `RTMR0..3` out of it, and returns them as the measurements JSON the WASM side expects — which is why the hex evidence starts with `{"measurements":`. If `attestation` fails while `platform-info` reports `IntelTdx`, the platform was detected but no quote could be produced: check that `/dev/tdx_guest` is present, and that `hal-runner` was built against a wasmhal with the `intel-tdx` feature. If it reports `type=AmdSev` on a TDX VM, the binary predates TDX support — rebuild it.

`hal-runner` embeds the HAL directly rather than going through proplet, so it carries its own wasmhal dependency. Keep it on the same rev as [`proplet/Cargo.toml`](../../proplet/Cargo.toml) — a stale pin is the usual reason the two disagree about what is supported.

## Part 7 — Delete the resources

The two instances and the firewall rule are the only billable or security-relevant things this guide creates. Run this from your workstation, in the shell that still has `$PROJECT` and `$ZONE` — **not** from inside either VM, since deleting the VM you are logged into will end the session mid-command.

```bash
gcloud compute instances delete propeller-intel-tdx-cvm --zone "$ZONE" --quiet
gcloud compute instances delete propeller-trustee-host   --zone "$ZONE" --quiet

gcloud compute firewall-rules delete allow-kbs-from-propeller \
  --project "$PROJECT" --quiet
```

If you skipped the optional firewall rule in [2.1](#21-provision-the-trustee-host), the last command fails with `not found`; that is fine, and the instances are the part that matters.

Both boot disks were created with auto-delete, so they go with their instances. Confirm nothing is left, especially if the instance was created from the console with a different name:

```bash
gcloud compute instances list --filter="name~propeller"
gcloud compute disks     list --filter="name~propeller"
gcloud compute firewall-rules list --filter="name~propeller"
```

An empty result from each means the project is back to where it started. A surviving disk usually means the console created it without `auto-delete=yes`, and it can be removed with `gcloud compute disks delete`.

Two things this does **not** clean up, because they are deliberately outside the resource group:

- **The KBS signing keys and the uploaded image key** live in `~/trustee` and in the KBS local-FS backend on the Trustee host. Deleting that host discards them. Nothing else can read the encrypted image if you do, which is the point.
- **The encrypted image in the registry** (`<you>/tee-wasm-addition:encrypted`) is not deleted by any of this. Remove it from the registry separately if you do not want it to stay.

## References

- [Trustee](https://github.com/confidential-containers/trustee)
- [Guest Components](https://github.com/confidential-containers/guest-components)
- [Propeller encrypted workloads guide](https://propeller.absmach.eu/docs/tee)
- [GCP Confidential VM supported configurations](https://cloud.google.com/confidential-computing/confidential-vm/docs/supported-configurations)
- [Creating a GCP Confidential VM instance](https://cloud.google.com/confidential-computing/confidential-vm/docs/create-a-confidential-vm-instance)
- [Trustee attestation policies](https://github.com/confidential-containers/trustee/blob/main/attestation-service/docs/policy.md)
- [Azure SEV-SNP CVM guide](../azure-sev/README.md)
- [GCP AMD SEV-SNP CVM guide](../gcp-sev/README.md)
