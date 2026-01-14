#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Propeller CVM Setup Script
# This script creates an Ubuntu-based CVM image with Proplet and Attestation Agent

set -e

# Configuration
BASE_IMAGE_URL="https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
BASE_IMAGE="ubuntu-base.qcow2"
CUSTOM_IMAGE="propeller-cvm.qcow2"
DISK_SIZE="40G"
SEED_IMAGE="seed.img"
USER_DATA="user-data"
META_DATA="meta-data"
VM_NAME="propeller-cvm"
RAM="18192M"
CPU="4"
USER="propeller"
PASSWORD="propeller"
QEMU_BINARY="qemu-system-x86_64"
OVMF_CODE="/usr/share/OVMF/OVMF_CODE.fd"
OVMF_VARS="/usr/share/OVMF/OVMF_VARS.fd"
OVMF_VARS_COPY="OVMF_VARS.fd"
ENABLE_CVM="${ENABLE_CVM:-auto}"  # Options: auto, tdx, sev, none

# Propeller Configuration (set these before running)
PROPLET_DOMAIN_ID="${PROPLET_DOMAIN_ID:-}"
PROPLET_CLIENT_ID="${PROPLET_CLIENT_ID:-}"
PROPLET_CLIENT_KEY="${PROPLET_CLIENT_KEY:-}"
PROPLET_CHANNEL_ID="${PROPLET_CHANNEL_ID:-}"
PROPLET_MQTT_ADDRESS="${PROPLET_MQTT_ADDRESS:-tcp://localhost:1883}"
KBS_URL="${KBS_URL:-https://kbs.example.com}"
KBS_PORT="${KBS_PORT:-8080}"

# Check prerequisites
if ! command -v wget &> /dev/null; then
  echo "wget is not installed. Please install it and try again."
  exit 1
fi

if ! command -v cloud-localds &> /dev/null; then
  echo "cloud-localds is not installed. Please install cloud-image-utils and try again."
  exit 1
fi

if ! command -v qemu-system-x86_64 &> /dev/null; then
  echo "qemu-system-x86_64 is not installed. Please install it and try again."
  exit 1
fi

if [[ $EUID -ne 0 ]]; then
   echo "This script must be run as root" 1>&2
   exit 1
fi

# Download base image if not present
if [ ! -f $BASE_IMAGE ]; then
  echo "Downloading base Ubuntu image..."
  wget -q $BASE_IMAGE_URL -O $BASE_IMAGE
fi

echo "Creating custom QEMU image..."
qemu-img create -f qcow2 -b $BASE_IMAGE -F qcow2 $CUSTOM_IMAGE $DISK_SIZE

# Create a writable copy of OVMF_VARS for this VM instance
if [ ! -f $OVMF_VARS_COPY ]; then
  echo "Creating OVMF vars copy..."
  cp $OVMF_VARS $OVMF_VARS_COPY
fi

# Generate instance ID
INSTANCE_ID=$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid)

# Create cloud-init user-data with embedded configuration
cat <<'EOF' > $USER_DATA
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
  - pkg-config
  - libssl-dev
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
    permissions: '0644'

  - path: /etc/default/attestation-agent
    content: |
      # Attestation Agent Environment Variables
      AA_LOG_LEVEL=info
      KBS_URL=KBS_URL_PLACEHOLDER
      KBS_PORT=KBS_PORT_PLACEHOLDER
      AA_ENDPOINT=127.0.0.1:50002
      AA_TOKEN_PATH=/run/attestation-agent/token
      AA_CA_CERT_PATH=/etc/attestation-agent/certs/ca.pem
      AA_TLS_INSECURE=false
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
      ExecStartPre=/bin/mkdir -p /etc/attestation-agent/certs
      ExecStart=/usr/local/bin/attestation-agent
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
  
  # Install TDX/SEV kernel support
  - |
    add-apt-repository -y ppa:kobuk-team/intel-tdx || echo "PPA add failed, trying canonical tdx"
    apt-get update || true
    apt-get install -y linux-image-generic linux-modules-extra-generic || echo "Kernel install failed"
    modprobe tdx_guest 2>/dev/null || echo "tdx_guest module not yet available (may need reboot)"
    mkdir -p /etc/modules-load.d
    echo "tdx_guest" > /etc/modules-load.d/tdx.conf
  
  # Create directories
  - mkdir -p /etc/attestation-agent/certs
  - mkdir -p /var/lib/proplet
  - mkdir -p /run/attestation-agent
  
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
      echo "✓ Wasmtime installed successfully"
      /usr/local/bin/wasmtime --version
    else
      echo "✗ ERROR: Wasmtime installation failed"
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
    git clone --depth 1 https://github.com/confidential-containers/guest-components.git
    cd guest-components/attestation-agent
    echo "Building attestation-agent with all attesters (this may take several minutes)..."
    if make ATTESTER=all-attesters 2>&1 | tee /tmp/aa-build.log; then
      if make install 2>&1 | tee -a /tmp/aa-build.log; then
        if [ -f /usr/local/bin/attestation-agent ]; then
          echo "✓ Attestation Agent built and installed successfully"
          /usr/local/bin/attestation-agent --help | head -5
        else
          echo "✗ ERROR: Installation succeeded but binary not found in /usr/local/bin/"
          exit 1
        fi
      else
        echo "✗ ERROR: Attestation Agent installation failed"
        cat /tmp/aa-build.log
        exit 1
      fi
    else
      echo "✗ ERROR: Attestation Agent build failed"
      cat /tmp/aa-build.log
      exit 1
    fi
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
    if cargo build --release 2>&1 | tee /tmp/proplet-build.log; then
      if [ -f target/release/proplet ]; then
        cp target/release/proplet /usr/local/bin/
        chmod +x /usr/local/bin/proplet
        echo "✓ Proplet built and installed successfully"
      else
        echo "✗ ERROR: Build succeeded but binary not found"
        exit 1
      fi
    else
      echo "✗ ERROR: Proplet build failed"
      cat /tmp/proplet-build.log
      exit 1
    fi
    cd /
    rm -rf /tmp/propeller
  
  # Verify binaries exist before enabling services
  - |
    echo "=== Verifying installations ==="
    ERRORS=0
    
    if [ ! -f /usr/local/bin/wasmtime ]; then
      echo "✗ ERROR: wasmtime binary not found"
      ERRORS=$((ERRORS + 1))
    else
      echo "✓ wasmtime: $(/usr/local/bin/wasmtime --version)"
    fi
    
    if [ ! -f /usr/local/bin/attestation-agent ]; then
      echo "✗ ERROR: attestation-agent binary not found"
      ERRORS=$((ERRORS + 1))
    else
      echo "✓ attestation-agent: installed"
    fi
    
    if [ ! -f /usr/local/bin/proplet ]; then
      echo "✗ ERROR: proplet binary not found"
      ERRORS=$((ERRORS + 1))
    else
      echo "✓ proplet: installed"
    fi
    
    if [ $ERRORS -gt 0 ]; then
      echo "✗ Installation verification failed with $ERRORS error(s)"
      echo "Services will NOT be started"
      exit 1
    fi
    
    echo "✓ All binaries verified successfully"
  
  # Enable and start services only if binaries exist
  - |
    echo "=== Enabling and starting services ==="
    systemctl daemon-reload
    systemctl enable attestation-agent.service
    systemctl enable proplet.service
    systemctl start attestation-agent.service
    sleep 2
    systemctl start proplet.service
    sleep 2
    
    echo "=== Service status ==="
    systemctl status attestation-agent.service --no-pager || true
    systemctl status proplet.service --no-pager || true

final_message: |
  ===================================================================
  Propeller CVM Setup Complete
  ===================================================================
  
  Services started:
    - Attestation Agent (port 50002)
    - Proplet (MQTT client)
  
  Login: propeller / propeller
  
  Check status:
    sudo systemctl status attestation-agent proplet
  
  View logs:
    sudo journalctl -u attestation-agent -f
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
sed -i "s|KBS_PORT_PLACEHOLDER|${KBS_PORT}|g" $USER_DATA

# Create meta-data
cat <<EOF > $META_DATA
instance-id: iid-${VM_NAME}
local-hostname: $VM_NAME
EOF

echo "Creating cloud-init seed image..."
cloud-localds $SEED_IMAGE $USER_DATA $META_DATA

# Detect CVM support
TDX_AVAILABLE=false
SEV_AVAILABLE=false

if [ "$ENABLE_CVM" = "auto" ] || [ "$ENABLE_CVM" = "tdx" ]; then
  if dmesg | grep -q "virt/tdx: module initialized"; then
    TDX_AVAILABLE=true
    echo "TDX host support detected"
  elif grep -q tdx /proc/cpuinfo; then
    TDX_AVAILABLE=true
    echo "TDX CPU support detected"
  fi
fi

if [ "$ENABLE_CVM" = "auto" ] || [ "$ENABLE_CVM" = "sev" ]; then
  if grep -q sev /proc/cpuinfo; then
    SEV_AVAILABLE=true
    echo "SEV CPU support detected"
  fi
fi

# Override if explicitly set
if [ "$ENABLE_CVM" = "tdx" ]; then
  TDX_AVAILABLE=true
  SEV_AVAILABLE=false
  echo "TDX mode forced via ENABLE_CVM=tdx"
elif [ "$ENABLE_CVM" = "sev" ]; then
  TDX_AVAILABLE=false
  SEV_AVAILABLE=true
  echo "SEV mode forced via ENABLE_CVM=sev"
elif [ "$ENABLE_CVM" = "none" ]; then
  TDX_AVAILABLE=false
  SEV_AVAILABLE=false
  echo "CVM disabled via ENABLE_CVM=none"
fi

# Build QEMU command
QEMU_CMD="$QEMU_BINARY"
QEMU_OPTS="-name $VM_NAME"
QEMU_OPTS="$QEMU_OPTS -m $RAM"
QEMU_OPTS="$QEMU_OPTS -smp $CPU"
QEMU_OPTS="$QEMU_OPTS -enable-kvm"
QEMU_OPTS="$QEMU_OPTS -boot d"
QEMU_OPTS="$QEMU_OPTS -netdev user,id=vmnic,hostfwd=tcp::2222-:22,hostfwd=tcp::50002-:50002"
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
echo "Full QEMU command:"
echo "$QEMU_CMD $QEMU_OPTS"
echo ""
echo "VM will be accessible via:"
echo "  SSH: ssh -p 2222 propeller@localhost"
echo "  Attestation Agent: localhost:50002"
echo ""
$QEMU_CMD $QEMU_OPTS
