#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Propeller CVM Setup Script
# This script creates an Ubuntu-based CVM image with Proplet and Attestation Agent

set -e

# Parse command line arguments
TARGET="${1:-all}"

if [[ "$TARGET" != "build" && "$TARGET" != "run" && "$TARGET" != "all" ]]; then
    echo "Usage: $0 [build|run|all]"
    echo ""
    echo "Targets:"
    echo "  build  - Build the CVM image and cloud-init configuration"
    echo "  run    - Boot the CVM (requires existing image)"
    echo "  all    - Build and run (default)"
    exit 1
fi

# Configuration
BASE_IMAGE_URL="https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
BASE_IMAGE="ubuntu-base.qcow2"
CUSTOM_IMAGE="propeller-cvm.qcow2"
DISK_SIZE="40G"
SEED_IMAGE="seed.img"
USER_DATA="user-data"
META_DATA="meta-data"
VM_NAME="propeller-cvm"
RAM="16384M"
CPU="4"
USER="propeller"
PASSWORD="propeller"
QEMU_BINARY="qemu-system-x86_64"
OVMF_CODE="/usr/share/OVMF/OVMF_CODE.fd"
OVMF_VARS="/usr/share/OVMF/OVMF_VARS.fd"
OVMF_VARS_COPY="OVMF_VARS.fd"
ENABLE_CVM="${ENABLE_CVM:-auto}" # Options: auto, tdx, sev, none

# Propeller Configuration (set these before running)
PROPLET_DOMAIN_ID="${PROPLET_DOMAIN_ID:-}"
PROPLET_CLIENT_ID="${PROPLET_CLIENT_ID:-}"
PROPLET_CLIENT_KEY="${PROPLET_CLIENT_KEY:-}"
PROPLET_CHANNEL_ID="${PROPLET_CHANNEL_ID:-}"
PROPLET_MQTT_ADDRESS="${PROPLET_MQTT_ADDRESS:-tcp://localhost:1883}"
KBS_URL="${KBS_URL:-http://10.0.2.2:8082}"

# Check prerequisites
check_prerequisites() {
    if ! command -v wget &>/dev/null; then
        echo "wget is not installed. Please install it and try again."
        exit 1
    fi

    if ! command -v cloud-localds &>/dev/null; then
        echo "cloud-localds is not installed. Please install cloud-image-utils and try again."
        exit 1
    fi

    if ! command -v qemu-system-x86_64 &>/dev/null; then
        echo "qemu-system-x86_64 is not installed. Please install it and try again."
        exit 1
    fi

    if [[ $EUID -ne 0 ]]; then
        echo "This script must be run as root" 1>&2
        exit 1
    fi
}

# Build CVM image and cloud-init configuration
build_cvm() {
    echo "=== Building CVM Image ==="

    # Download base image if not present
    if [ ! -f $BASE_IMAGE ]; then
        wget -q $BASE_IMAGE_URL -O $BASE_IMAGE
    fi

    qemu-img create -f qcow2 -b $BASE_IMAGE -F qcow2 $CUSTOM_IMAGE $DISK_SIZE

    # Create a writable copy of OVMF_VARS for this VM instance
    if [ ! -f $OVMF_VARS_COPY ]; then
        cp $OVMF_VARS $OVMF_VARS_COPY
    fi

    # Generate instance ID
    INSTANCE_ID=$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid)

    # Create cloud-init user-data with embedded configuration
    cat <<'EOF' >$USER_DATA
#cloud-config
# Propeller CVM Cloud-Init Configuration
# Installs Proplet, Attestation Agent, and Wasmtime

package_update: true
package_upgrade: false

users:
  - name: propeller
    plain_text_passwd: propeller
    lock_passwd: false
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    groups: sudo

ssh_pwauth: true

packages:
  # Build essentials
  - build-essential
  - gcc
  - make
  - pkg-config
  - libssl-dev
  - openssl
  - curl
  - git
  - wget
  - ca-certificates
  # MQTT and networking
  - mosquitto-clients
  - libmosquitto-dev
  # Additional dependencies
  - jq
  - unzip
  - protobuf-compiler
  - libprotobuf-dev
  # TPM and attestation dependencies
  - libtss2-dev
  - tpm2-tools
  # Debug tools
  - rustup

write_files:
  - path: /etc/default/proplet
    content: |
      # Proplet Environment Variables
      PROPLET_LOG_LEVEL=info
      PROPLET_INSTANCE_ID=INSTANCE_ID_PLACEHOLDER
      PROPLET_DOMAIN_ID=DOMAIN_ID_PLACEHOLDER
      PROPLET_CLIENT_ID=CLIENT_ID_PLACEHOLDER
      PROPLET_CLIENT_KEY=CLIENT_KEY_PLACEHOLDER
      PROPLET_CHANNEL_ID=CHANNEL_ID_PLACEHOLDER
      PROPLET_MQTT_ADDRESS=MQTT_ADDRESS_PLACEHOLDER
      PROPLET_MQTT_TIMEOUT=30
      PROPLET_MQTT_QOS=2
      PROPLET_EXTERNAL_WASM_RUNTIME=/usr/local/bin/wasmtime
      PROPLET_LIVELINESS_INTERVAL=10
      PROPLET_MANAGER_K8S_NAMESPACE=default
      PROPLET_CONFIG_FILE=/etc/proplet/config.toml
      PROPLET_CONFIG_SECTION=proplet1
    permissions: '0644'

  - path: /etc/proplet/config.toml
    content: |
      # SuperMQ Configuration

      [manager]
      domain_id = "4bae1a76-afc4-4054-976c-5427c49fbbf3"
      client_id = "cdaccb11-7209-4fb9-8df1-3c52e9d64284"
      client_key = "507d687d-51f8-4c71-8599-4273a5d75429"
      channel_id = "34a616c3-8817-4995-aade-a383e64766a8"

      [proplet1]
      domain_id = "4bae1a76-afc4-4054-976c-5427c49fbbf3"
      client_id = "0deb859f-973d-4e2e-93cf-ec756f4fc3c8"
      client_key = "17c03d05-b55d-4a05-88ec-cadecb2130c4"
      channel_id = "34a616c3-8817-4995-aade-a383e64766a8"

      [proplet2]
      domain_id = "4bae1a76-afc4-4054-976c-5427c49fbbf3"
      client_id = "3dfb6fa7-8e8f-4a2b-a462-8afc59898686"
      client_key = "06244015-8286-4dd6-89bd-e2ba7d7a9637"
      channel_id = "34a616c3-8817-4995-aade-a383e64766a8"

      [proplet3]
      domain_id = "4bae1a76-afc4-4054-976c-5427c49fbbf3"
      client_id = "f869bde7-8b1a-483e-9837-b621309af55a"
      client_key = "316ba339-76f5-4149-acd7-8d6f3f7c9276"
      channel_id = "34a616c3-8817-4995-aade-a383e64766a8"

      [proxy]
      domain_id = "4bae1a76-afc4-4054-976c-5427c49fbbf3"
      client_id = "0deb859f-973d-4e2e-93cf-ec756f4fc3c8"
      client_key = "17c03d05-b55d-4a05-88ec-cadecb2130c4"
      channel_id = "34a616c3-8817-4995-aade-a383e64766a8"
    permissions: '0644'

  - path: /etc/default/attestation-agent
    content: |
      # Attestation Agent Environment Variables
      # The attester type (TDX, SEV, etc.) will be auto-detected
      AA_ATTESTATION_SOCK=127.0.0.1:50010
      RUST_LOG=info
    permissions: '0644'

  - path: /etc/default/coco-keyprovider
    content: |
      # CoCo Keyprovider Environment Variables
      COCO_KP_SOCKET=127.0.0.1:50011
      COCO_KP_KBS_URL=http://10.0.2.2:8082
      RUST_LOG=info
    permissions: '0644'

  - path: /etc/ocicrypt_keyprovider.conf
    content: |
      {
        "key-providers": {
          "attestation-agent": {
            "grpc": "127.0.0.1:50011"
          }
        }
      }
    permissions: '0644'

  - path: /etc/systemd/system/attestation-agent.service
    content: |
      [Unit]
      Description=Attestation Agent for Confidential Containers
      Documentation=https://github.com/confidential-containers/guest-components
      After=network-online.target
      Wants=network-online.target

      [Service]
      Type=simple
      EnvironmentFile=/etc/default/attestation-agent
      ExecStartPre=/bin/mkdir -p /run/attestation-agent
      ExecStart=/usr/local/bin/attestation-agent --attestation_sock ${AA_ATTESTATION_SOCK}
      Restart=on-failure
      RestartSec=5s
      StandardOutput=journal
      StandardError=journal

      NoNewPrivileges=true
      PrivateTmp=true
      ProtectSystem=strict
      ProtectHome=true
      ReadWritePaths=/run/attestation-agent /etc/attestation-agent

      [Install]
      WantedBy=multi-user.target
    permissions: '0644'

  - path: /etc/systemd/system/coco-keyprovider.service
    content: |
      [Unit]
      Description=CoCo Keyprovider for Confidential Containers
      Documentation=https://github.com/confidential-containers/guest-components
      After=network-online.target
      Wants=network-online.target

      [Service]
      Type=simple
      EnvironmentFile=/etc/default/coco-keyprovider
      ExecStart=/usr/local/bin/coco_keyprovider --socket ${COCO_KP_SOCKET} --kbs ${COCO_KP_KBS_URL}
      Restart=on-failure
      RestartSec=5s
      StandardOutput=journal
      StandardError=journal

      NoNewPrivileges=true
      PrivateTmp=true
      ProtectSystem=strict
      ProtectHome=true
      ReadWritePaths=/run/coco-keyprovider

      [Install]
      WantedBy=multi-user.target
    permissions: '0644'

  - path: /etc/systemd/system/proplet.service
    content: |
      [Unit]
      Description=Proplet WebAssembly Workload Orchestrator
      Documentation=https://github.com/absmach/propeller
      After=network-online.target attestation-agent.service
      Wants=network-online.target

      [Service]
      Type=simple
      EnvironmentFile=/etc/default/proplet
      ExecStart=/usr/local/bin/proplet
      Restart=on-failure
      RestartSec=5s
      StandardOutput=journal
      StandardError=journal

      NoNewPrivileges=true
      PrivateTmp=true
      ProtectSystem=strict
      ProtectHome=true
      ReadWritePaths=/var/lib/proplet /tmp

      [Install]
      WantedBy=multi-user.target
    permissions: '0644'

runcmd:
  # Set user password
  - echo 'propeller:propeller' | chpasswd

  # Enable SSH password authentication
  - |
    cat > /etc/ssh/sshd_config.d/60-cloudimg-settings.conf <<'SSHEOF'
    PasswordAuthentication yes
    SSHEOF
  - systemctl restart sshd
  - sleep 2

  # Install TDX kernel modules (standard Ubuntu 24.04 kernel includes TDX support)
  - |
    echo "=== Installing TDX kernel modules ==="
    # The standard Ubuntu 24.04 kernel already includes TDX support
    # We need to install linux-modules-extra for the current kernel
    KERNEL_VERSION=$(uname -r)
    echo "Current kernel: $KERNEL_VERSION"

    apt-get update
    apt-get install -y "linux-modules-extra-${KERNEL_VERSION}" || {
      echo "Failed to install modules-extra for current kernel, trying generic"
      apt-get install -y linux-modules-extra-generic
    }

    # Configure tdx_guest module to load at boot
    mkdir -p /etc/modules-load.d
    echo "tdx_guest" > /etc/modules-load.d/tdx.conf

    # Load the module now
    modprobe tdx_guest 2>/dev/null && echo "tdx_guest module loaded successfully" || echo "tdx_guest module will load on next boot"

    # Verify the device exists
    if [ -e /dev/tdx_guest ]; then
      echo "/dev/tdx_guest device created"
    else
      echo "Note: /dev/tdx_guest will be available after module loads"
    fi

  # Create directories
  - mkdir -p /etc/attestation-agent/certs
  - mkdir -p /var/lib/proplet
  - mkdir -p /etc/proplet
  - mkdir -p /run/attestation-agent
  - mkdir -p /run/coco-keyprovider

  # Install Wasmtime
  - |
    echo "=== Installing Wasmtime ==="
    WASMTIME_VERSION=$(curl -s https://api.github.com/repos/bytecodealliance/wasmtime/releases/latest | jq -r .tag_name)
    echo "Downloading Wasmtime ${WASMTIME_VERSION}..."
    curl -L "https://github.com/bytecodealliance/wasmtime/releases/download/${WASMTIME_VERSION}/wasmtime-${WASMTIME_VERSION}-x86_64-linux.tar.xz" -o /tmp/wasmtime.tar.xz
    tar -xf /tmp/wasmtime.tar.xz -C /tmp
    mv /tmp/wasmtime-${WASMTIME_VERSION}-x86_64-linux/wasmtime /usr/local/bin/
    chmod +x /usr/local/bin/wasmtime
    rm -rf /tmp/wasmtime*
    if [ -f /usr/local/bin/wasmtime ]; then
      echo "Wasmtime installed successfully"
      /usr/local/bin/wasmtime --version
    else
      echo "ERROR: Wasmtime installation failed"
      exit 1
    fi

  # Install Rust toolchain (needed for building from source)
  - |
    echo "=== Installing Rust toolchain ==="
    export HOME=/root
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
    export PATH="/root/.cargo/bin:$PATH"
    echo 'export PATH="/root/.cargo/bin:$PATH"' >> /root/.bashrc
    rustc --version
    cargo --version

  # Build and install Attestation Agent
  - |
    echo "=== Building Attestation Agent from source ==="
    export HOME=/root
    export PATH="/root/.cargo/bin:$PATH"
    cd /tmp
    git clone https://github.com/rodneyosodo/guest-components.git
    cd guest-components
    git checkout upstream-proplet
    cd attestation-agent
    echo "Building attestation-agent (gRPC version) with all attesters (this may take several minutes)..."
    make ATTESTER=all-attesters ttrpc=false
    make install
    /usr/local/bin/attestation-agent --help
    cd /
    rm -rf /tmp/guest-components

  # Build and install CoCo Keyprovider
  - |
    echo "=== Building CoCo Keyprovider from source ==="
    export HOME=/root
    export PATH="/root/.cargo/bin:$PATH"
    cd /tmp
    git clone https://github.com/rodneyosodo/guest-components.git
    cd guest-components
    git checkout upstream-proplet
    cd attestation-agent/coco_keyprovider
    echo "Building CoCo Keyprovider (this may take several minutes)..."
    cargo build --release --target x86_64-unknown-linux-gnu
    chmod +x ../../target/x86_64-unknown-linux-gnu/release/coco_keyprovider
    cp ../../target/x86_64-unknown-linux-gnu/release/coco_keyprovider /usr/local/bin/
    /usr/local/bin/coco_keyprovider --help
    echo "CoCo Keyprovider built and installed successfully"
    cd /
    rm -rf /tmp/guest-components

  # Build and install Proplet
  - |
    echo "=== Building Proplet from source ==="
    export HOME=/root
    export PATH="/root/.cargo/bin:$PATH"
    cd /tmp
    git clone --depth 1 https://github.com/absmach/propeller.git
    cd propeller/proplet
    echo "Building proplet (this may take several minutes)..."
    cargo build --release
    chmod +x target/release/proplet
    cp target/release/proplet /usr/local/bin/
    cd /
    rm -rf /tmp/propeller

  # Verify binaries exist before enabling services
  - |
    echo "=== Verifying installations ==="
    ERRORS=0

    if [ ! -f /usr/local/bin/wasmtime ]; then
      echo "ERROR: wasmtime binary not found"
      ERRORS=$((ERRORS + 1))
    else
      echo "wasmtime: $(/usr/local/bin/wasmtime --version)"
    fi

    if [ ! -f /usr/local/bin/attestation-agent ]; then
      echo "ERROR: attestation-agent binary not found"
      ERRORS=$((ERRORS + 1))
    else
      echo "attestation-agent: installed"
    fi

    if [ ! -f /usr/local/bin/coco_keyprovider ]; then
      echo "ERROR: coco_keyprovider binary not found"
      ERRORS=$((ERRORS + 1))
    else
      echo "coco_keyprovider: installed"
    fi

    if [ ! -f /usr/local/bin/proplet ]; then
      echo "ERROR: proplet binary not found"
      ERRORS=$((ERRORS + 1))
    else
      echo "proplet: installed"
    fi

    if [ ! -f /etc/ocicrypt_keyprovider.conf ]; then
      echo "ERROR: /etc/ocicrypt_keyprovider.conf not found"
      ERRORS=$((ERRORS + 1))
    else
      echo "ocicrypt_keyprovider.conf: created"
    fi

    if [ $ERRORS -gt 0 ]; then
      echo "Installation verification failed with $ERRORS error(s)"
      echo "Services will NOT be started"
      exit 1
    fi

    echo "All binaries verified successfully"

  # Enable and start services only if binaries exist
  - |
    echo "=== Enabling and starting services ==="
    systemctl daemon-reload
    systemctl enable attestation-agent.service
    systemctl enable coco-keyprovider.service
    systemctl enable proplet.service
    systemctl start attestation-agent.service
    sleep 2
    systemctl start coco-keyprovider.service
    sleep 2
    systemctl start proplet.service
    sleep 2

    echo "=== Service status ==="
    systemctl status attestation-agent.service --no-pager || true
    systemctl status coco-keyprovider.service --no-pager || true
    systemctl status proplet.service --no-pager || true

final_message: |
  ===================================================================
  Propeller CVM Setup Complete
  ===================================================================

  Services started:
    - Attestation Agent (port 50010)
    - CoCo Keyprovider (port 50011)
    - Proplet (MQTT client)

  Login: propeller / propeller

  Check status:
    sudo systemctl status attestation-agent coco-keyprovider proplet

  View logs:
    sudo journalctl -u attestation-agent -f
    sudo journalctl -u coco-keyprovider -f
    sudo journalctl -u proplet -f

  ===================================================================
EOF

    # Substitute configuration values in user-data
    sed -i "s|INSTANCE_ID_PLACEHOLDER|${INSTANCE_ID}|g" $USER_DATA
    sed -i "s|DOMAIN_ID_PLACEHOLDER|${PROPLET_DOMAIN_ID}|g" $USER_DATA
    sed -i "s|CLIENT_ID_PLACEHOLDER|${PROPLET_CLIENT_ID}|g" $USER_DATA
    sed -i "s|CLIENT_KEY_PLACEHOLDER|${PROPLET_CLIENT_KEY}|g" $USER_DATA
    sed -i "s|CHANNEL_ID_PLACEHOLDER|${PROPLET_CHANNEL_ID}|g" $USER_DATA
    sed -i "s|MQTT_ADDRESS_PLACEHOLDER|${PROPLET_MQTT_ADDRESS}|g" $USER_DATA
    sed -i "s|KBS_URL_PLACEHOLDER|${KBS_URL}|g" $USER_DATA

    # Create meta-data
    cat <<EOF >$META_DATA
instance-id: iid-${VM_NAME}
local-hostname: $VM_NAME
EOF

    cloud-localds $SEED_IMAGE $USER_DATA $META_DATA

    echo "CVM image build complete!"
}

# Detect CVM support and build QEMU command
detect_cvm_support() {
    TDX_AVAILABLE=false
    SEV_AVAILABLE=false

    if [ "$ENABLE_CVM" = "auto" ] || [ "$ENABLE_CVM" = "tdx" ]; then
        if dmesg | grep -q "virt/tdx: module initialized"; then
            TDX_AVAILABLE=true
        elif grep -q tdx /proc/cpuinfo; then
            TDX_AVAILABLE=true
        fi
    fi

    if [ "$ENABLE_CVM" = "auto" ] || [ "$ENABLE_CVM" = "sev" ]; then
        if grep -q sev /proc/cpuinfo; then
            SEV_AVAILABLE=true
        fi
    fi

    # Override if explicitly set
    if [ "$ENABLE_CVM" = "tdx" ]; then
        TDX_AVAILABLE=true
        SEV_AVAILABLE=false
    elif [ "$ENABLE_CVM" = "sev" ]; then
        TDX_AVAILABLE=false
        SEV_AVAILABLE=true
    elif [ "$ENABLE_CVM" = "none" ]; then
        TDX_AVAILABLE=false
        SEV_AVAILABLE=false
    fi
}

# Run the CVM
run_cvm() {
    echo "=== Running CVM ==="

    # Check if required files exist
    if [ ! -f "$CUSTOM_IMAGE" ]; then
        echo "Error: CVM image not found: $CUSTOM_IMAGE"
        echo "Please run '$0 build' first to create the image."
        exit 1
    fi

    if [ ! -f "$SEED_IMAGE" ]; then
        echo "Error: Cloud-init seed image not found: $SEED_IMAGE"
        echo "Please run '$0 build' first to create the seed image."
        exit 1
    fi

    # Detect CVM support
    detect_cvm_support

    # Build QEMU command
    QEMU_CMD="$QEMU_BINARY"
    QEMU_OPTS="-name $VM_NAME"
    QEMU_OPTS="$QEMU_OPTS -m $RAM"
    QEMU_OPTS="$QEMU_OPTS -smp $CPU"
    QEMU_OPTS="$QEMU_OPTS -enable-kvm"
    QEMU_OPTS="$QEMU_OPTS -boot d"
    QEMU_OPTS="$QEMU_OPTS -netdev user,id=vmnic,hostfwd=tcp::2222-:22,hostfwd=tcp::50010-:50010,hostfwd=tcp::50011-:50011"
    QEMU_OPTS="$QEMU_OPTS -nographic"
    QEMU_OPTS="$QEMU_OPTS -no-reboot"
    QEMU_OPTS="$QEMU_OPTS -drive file=$SEED_IMAGE,media=cdrom"
    QEMU_OPTS="$QEMU_OPTS -drive file=$CUSTOM_IMAGE,if=none,id=disk0,format=qcow2"
    QEMU_OPTS="$QEMU_OPTS -device virtio-scsi-pci,id=scsi,disable-legacy=on"
    QEMU_OPTS="$QEMU_OPTS -device scsi-hd,drive=disk0"

    if [ "$TDX_AVAILABLE" = true ]; then
        echo "Starting QEMU VM with Intel TDX (Confidential VM)..."
        QEMU_OPTS=$(echo "$QEMU_OPTS" | sed "s/-name $VM_NAME/-name $VM_NAME,process=$VM_NAME,debug-threads=on/")
        QEMU_OPTS=$(echo "$QEMU_OPTS" | sed "s/-m $RAM//")
        QEMU_OPTS="$QEMU_OPTS -object memory-backend-memfd,id=ram1,size=$RAM,share=true,prealloc=false"
        QEMU_OPTS="$QEMU_OPTS -m $RAM"
        QEMU_OPTS="$QEMU_OPTS -cpu host,pmu=off"
        QEMU_OPTS="$QEMU_OPTS -object {\"qom-type\":\"tdx-guest\",\"id\":\"tdx0\",\"quote-generation-socket\":{\"type\":\"vsock\",\"cid\":\"2\",\"port\":\"4050\"}}"
        QEMU_OPTS="$QEMU_OPTS -machine q35,confidential-guest-support=tdx0,memory-backend=ram1,kernel-irqchip=split,hpet=off"
        QEMU_OPTS="$QEMU_OPTS -bios /usr/share/ovmf/OVMF.fd"
        QEMU_OPTS="$QEMU_OPTS -device virtio-net-pci,disable-legacy=on,iommu_platform=true,netdev=vmnic,romfile="
        QEMU_OPTS="$QEMU_OPTS -nodefaults"
        QEMU_OPTS="$QEMU_OPTS -nographic"
        QEMU_OPTS="$QEMU_OPTS -serial mon:stdio"
        QEMU_OPTS="$QEMU_OPTS -monitor pty"
    elif [ "$SEV_AVAILABLE" = true ]; then
        echo "Starting QEMU VM with AMD SEV (Confidential VM)..."
        QEMU_OPTS="$QEMU_OPTS -machine q35"
        QEMU_OPTS="$QEMU_OPTS -cpu EPYC"
        QEMU_OPTS="$QEMU_OPTS -object sev-guest,id=sev0,cbitpos=47,reduced-phys-bits=1"
        QEMU_OPTS="$QEMU_OPTS -machine memory-encryption=sev0"
        QEMU_OPTS="$QEMU_OPTS -drive if=pflash,format=raw,unit=0,file=$OVMF_CODE,readonly=on"
        QEMU_OPTS="$QEMU_OPTS -drive if=pflash,format=raw,unit=1,file=$OVMF_VARS_COPY"
        QEMU_OPTS="$QEMU_OPTS -device virtio-net-pci,netdev=vmnic,romfile="
    else
        echo "Starting QEMU VM in regular mode (no CVM)..."
        QEMU_OPTS="$QEMU_OPTS -drive if=pflash,format=raw,unit=0,file=$OVMF_CODE,readonly=on"
        QEMU_OPTS="$QEMU_OPTS -drive if=pflash,format=raw,unit=1,file=$OVMF_VARS_COPY"
        QEMU_OPTS="$QEMU_OPTS -cpu host"
        QEMU_OPTS="$QEMU_OPTS -machine q35"
        QEMU_OPTS="$QEMU_OPTS -device virtio-net-pci,netdev=vmnic,romfile="
    fi

    # Execute QEMU
    echo "VM will be accessible via:"
    echo "  SSH: ssh -p 2222 propeller@localhost"
    echo "  Attestation Agent: localhost:50010"
    echo "  CoCo Keyprovider: localhost:50011"
    echo ""
    $QEMU_CMD $QEMU_OPTS
}

# Main execution
check_prerequisites

case "$TARGET" in
build)
    build_cvm
    ;;
run)
    run_cvm
    ;;
all)
    build_cvm
    run_cvm
    ;;
esac
