# Proxy Service

The Proxy Service acts as a bridge between MQTT and HTTP protocols in the Propeller system. It enables bidirectional communication between MQTT clients and HTTP endpoints, allowing for seamless integration of different protocols.

## Overview

The proxy service performs two main functions:
1. Subscribes to MQTT topics and forwards messages to HTTP endpoints
2. Streams data between MQTT and HTTP protocols

## Configuration

The service is configured using environment variables.

### Environment Variables

#### MQTT Configuration

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `BrokerURL` | URL of the MQTT broker | `localhost:1883` | Yes |
| `PropletID` | Unique identifier for the proplet | `72fd490b-f91f-47dc-aa0b-a65931719ee1` | Yes |
| `ChannelID` | Channel identifier for MQTT communication | `cb6cb9ae-ddcf-41ab-8f32-f3e93b3a3be2` | Yes |
| `PropletPassword` | Password for MQTT authentication | `3963a940-332e-4a18-aa57-bab4d4124ab0` | Yes |

#### Registry Configuration

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `RegistryURL` | URL of the HTTP registry | `localhost:5000` | Yes |
| `Authenticate` | Enable/disable registry authentication | `false` | No |
| `RegistryUsername` | Username for registry authentication | `""` | Only if `Authenticate=true` |
| `RegistryPassword` | Password for registry authentication | `""` | Only if `Authenticate=true` |

### Example Configuration
```env
# MQTT Configuration
BrokerURL=localhost:1883
PropletID=72fd490b-f91f-47dc-aa0b-a65931719ee1
ChannelID=cb6cb9ae-ddcf-41ab-8f32-f3e93b3a3be2
PropletPassword=3963a940-332e-4a18-aa57-bab4d4124ab0

# Registry Configuration
RegistryURL=localhost:5000
Authenticate=false
RegistryUsername=
RegistryPassword=
```

## Running the Service

The proxy service can be started by running the main.go file:

```bash
go run cmd/proxy/main.go
```

## Service Flow

1. **Initialization**
   - Loads configuration from environment variables
   - Sets up logging
   - Creates a new proxy service instance

2. **Connection**
   - Establishes connection to the MQTT broker
   - Subscribes to configured topics
   - Sets up HTTP streaming

3. **Operation**
   - Runs two concurrent streams:
     - StreamHTTP: Handles HTTP communication
     - StreamMQTT: Handles MQTT communication
   - Uses error groups for graceful error handling and shutdown

4. **Error Handling**
   - Implements comprehensive error logging
   - Graceful shutdown with proper resource cleanup
   - Automatic disconnection from MQTT broker on service termination

## HTTP Registry Operations

The HTTP configuration supports:
- Registry operations with optional authentication
- Automatic retry mechanism for failed requests
- Chunked data handling with configurable chunk size (1MB default)
- Static credential caching for authenticated requests
