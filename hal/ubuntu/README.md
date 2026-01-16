# Propeller CVM Setup with QEMU

This directory contains scripts for building and running Propeller CVMs (Confidential VMs) using QEMU.

## Quick Start

### Prerequisites

Install required packages:

```bash
sudo apt-get update
sudo apt-get install -y \
  qemu-system-x86 \
  cloud-image-utils \
  ovmf \
  wget
```

### Basic Usage

First, make the script executable:

```bash
chmod +x qemu.sh
```

The script supports three targets:

```bash
# Build and run (default)
sudo ./qemu.sh

# Build only - creates the CVM image and cloud-init configuration
sudo ./qemu.sh build

# Run only - boots an existing CVM image
sudo ./qemu.sh run
```

**Example workflow:**

```bash
# Set required configuration
export PROPLET_DOMAIN_ID="your-domain-id"
export PROPLET_CLIENT_ID="your-client-id"
export PROPLET_CLIENT_KEY="your-client-key"
export PROPLET_CHANNEL_ID="your-channel-id"
export PROPLET_MQTT_ADDRESS="tcp://mqtt.example.com:1883"
export KBS_URL="https://kbs.example.com"

# Build the CVM image (only needs to be done once)
sudo ./qemu.sh build

# Run the CVM (can be done multiple times)
sudo ./qemu.sh run
```

### CVM Modes

The script supports different CVM modes via the `ENABLE_CVM` environment variable:

```bash
# Auto-detect (default) - uses TDX if available, otherwise regular VM
sudo ./qemu.sh

# Force Intel TDX
sudo ENABLE_CVM=tdx ./qemu.sh

# Force AMD SEV
sudo ENABLE_CVM=sev ./qemu.sh

# Disable CVM (regular VM)
sudo ENABLE_CVM=none ./qemu.sh
```


## Configuration

### Required Environment Variables

| Variable             | Description               | Example               |
|----------------------|---------------------------|-----------------------|
| `PROPLET_DOMAIN_ID`  | SuperMQ domain identifier | `my-domain-123`       |
| `PROPLET_CLIENT_ID`  | Unique client identifier  | `proplet-worker-01`   |
| `PROPLET_CLIENT_KEY` | Authentication key        | `secret-key-here`     |
| `PROPLET_CHANNEL_ID` | Communication channel ID  | `channel-456`         |

### Optional Environment Variables

| Variable               | Description                  | Default                   |
|------------------------|------------------------------|---------------------------|
| `PROPLET_MQTT_ADDRESS` | MQTT broker address          | `tcp://localhost:1883`    |
| `KBS_URL`              | Key Broker Service URL       | `https://kbs.example.com` |
| `KBS_PORT`             | KBS port                     | `8080`                    |
| `ENABLE_CVM`           | CVM mode (auto/tdx/sev/none) | `auto`                    |
| `RAM`                  | VM memory                    | `8192M`                   |
| `CPU`                  | CPU cores                    | `4`                       |
| `DISK_SIZE`            | Disk size                    | `40G`                     |

## What the Script Does

### Build Phase (`./qemu.sh build`)

1. **Downloads Ubuntu Cloud Image**: Fetches the latest Ubuntu Noble cloud image
2. **Creates Custom Image**: Creates a QCOW2 image with specified disk size
3. **Generates Cloud-Init Configuration**: Creates user-data and meta-data files with:
   - User credentials
   - Package installation list
   - Service configurations
   - Build scripts for components
4. **Creates Seed Image**: Packages cloud-init configs into an ISO image

The cloud-init configuration will install and build (on first boot):
- Rust toolchain
- Wasmtime runtime
- Attestation Agent
- Proplet
- Systemd services for AA and Proplet

### Run Phase (`./qemu.sh run`)

1. **Detects CVM Support**: Checks for Intel TDX or AMD SEV capabilities
2. **Builds QEMU Command**: Constructs appropriate QEMU arguments based on CVM mode
3. **Boots VM**: Launches QEMU with the configured settings
4. **First Boot**: Cloud-init runs and builds all components (takes ~10-15 minutes)
5. **Subsequent Boots**: Services start immediately with pre-built binaries

## VM Access

After the VM boots:

```bash
# SSH access
ssh -p 2222 propeller@localhost
# Password: propeller

# Check service status
sudo systemctl status attestation-agent proplet

# View logs
sudo journalctl -u attestation-agent -f
sudo journalctl -u proplet -f
```

## Network Ports

The VM exposes:
- **Port 2222**: SSH (forwarded from guest port 22)
- **Port 50002**: Attestation Agent API (forwarded from guest port 50002)

## Files Created

The script creates the following files in the current directory:

- `ubuntu-base.qcow2` - Downloaded base Ubuntu image
- `propeller-cvm.qcow2` - Custom VM image with Propeller components
- `seed.img` - Cloud-init seed image
- `user-data` - Cloud-init user data (temporary)
- `meta-data` - Cloud-init metadata (temporary)
- `OVMF_VARS.fd` - UEFI variables (per-VM copy)

## Intel TDX Requirements

For Intel TDX support, the host must have:

1. **TDX-capable CPU**: Intel Xeon Scalable (Sapphire Rapids or later)
2. **TDX-enabled BIOS**: TDX must be enabled in BIOS settings
3. **TDX-enabled Kernel**: Linux kernel with TDX support
4. **TDX Kernel Module**: `tdx` module loaded

Check TDX availability:

```bash
# Check CPU support
grep tdx /proc/cpuinfo

# Check kernel module
dmesg | grep -i tdx

# Check if TDX is initialized
dmesg | grep "virt/tdx: module initialized"
```

## AMD SEV Requirements

For AMD SEV support, the host must have:

1. **SEV-capable CPU**: AMD EPYC processor
2. **SEV-enabled BIOS**: SEV must be enabled in BIOS
3. **SEV-enabled Kernel**: Linux kernel with SEV support

Check SEV availability:

```bash
# Check CPU support
grep sev /proc/cpuinfo

# Check SEV support
dmesg | grep -i sev
```

## Troubleshooting

### Script fails with "must be run as root"

The script requires root privileges to run QEMU with KVM:

```bash
sudo ./qemu.sh
```

### Cloud-init not completing

Monitor cloud-init progress inside the VM:

```bash
ssh -p 2222 propeller@localhost
sudo cloud-init status --wait
sudo tail -f /var/log/cloud-init-output.log
```

### Services not starting

Check if credentials are properly set:

```bash
ssh -p 2222 propeller@localhost
cat /etc/default/proplet
cat /etc/default/attestation-agent
```

### TDX/SEV not working

Force regular mode to test basic functionality:

```bash
sudo ENABLE_CVM=none ./qemu.sh
```

## Architecture

The VM includes:

```
┌─────────────────────────────────────────────┐
│           Propeller CVM (Ubuntu)            │
│                                             │
│  ┌──────────────┐      ┌────────────────┐   │
│  │ Attestation  │      │    Wasmtime    │   │
│  │    Agent     │      │   (Runtime)    │   │
│  │ (port 50002) │      └────────────────┘   │
│  └──────────────┘               ▲           │
│         │                       │           │
│         │                       │           │
│  ┌──────▼───────────────────────┴────────┐  │
│  │           Proplet                     │  │
│  │  (WebAssembly Orchestrator)           │  │
│  └───────────────────────────────────────┘  │
│                    │                        │
└────────────────────┼────────────────────────┘
                     │ MQTT
                     ▼
              SuperMQ Broker
```

## Advanced Usage

### Custom VM Configuration

Edit the script variables at the top:

```bash
RAM="16384M"      # Increase RAM
CPU="8"           # More CPU cores
DISK_SIZE="100G"  # Larger disk
```

### Using Pre-built Binaries

To skip compilation, modify the `runcmd` section in the embedded cloud-init to download pre-built binaries instead of building from source.

### Multiple VMs

Run multiple instances by changing the VM name:

```bash
VM_NAME="propeller-cvm-1" sudo ./qemu.sh &
VM_NAME="propeller-cvm-2" sudo ./qemu.sh &
```

Note: You'll need to adjust port forwarding to avoid conflicts.

## See Also

- [Propeller Documentation](https://docs.propeller.absmach.eu/)
- [Proplet README](../../proplet/README.md)
- [Confidential Containers](https://confidentialcontainers.org/)
- [Intel TDX Documentation](https://www.intel.com/content/www/us/en/developer/tools/trust-domain-extensions/overview.html)
- [AMD SEV Documentation](https://developer.amd.com/sev/)
