# CVM Setup - Moved

**Note**: The cloud-init setup documentation has been reorganized.

The CVM setup is now located in the `hal/ubuntu/` directory with a QEMU-based build script that follows the same pattern as other confidential computing projects.

## New Location

📚 **[hal/ubuntu/README.md](../hal/ubuntu/README.md)** - Complete CVM setup guide

## What Changed

The setup has been refactored from a standalone cloud-init YAML file to a comprehensive QEMU build script:

- **Old approach**: Standalone `cloud-init-cvm.yaml` file for cloud providers
- **New approach**: `hal/ubuntu/qemu.sh` script that builds and boots a CVM locally

## Quick Start

```bash
# Set configuration
export PROPLET_DOMAIN_ID="your-domain-id"
export PROPLET_CLIENT_ID="your-client-id"
export PROPLET_CLIENT_KEY="your-client-key"
export PROPLET_CHANNEL_ID="your-channel-id"
export PROPLET_MQTT_ADDRESS="tcp://mqtt.example.com:1883"

# Build and run CVM
cd hal/ubuntu
sudo ./qemu.sh
```

## Benefits of New Approach

1. **Self-contained**: Everything in one script
2. **Local testing**: Build and test CVMs locally with QEMU
3. **CVM support**: Built-in Intel TDX and AMD SEV support
4. **Embedded config**: No separate template files needed
5. **Follows patterns**: Consistent with cube and other CoCo projects

See the [new documentation](../hal/ubuntu/README.md) for complete details.
