# Quick Start Guide - Federated Learning Demo

## Quick Commands Reference

### 1. Build WASM Client
```bash
cd /home/jeff-mboya/Documents/propeller/examples/fl-demo/client-wasm
GOOS=wasip1 GOARCH=wasm go build -o fl-client.wasm fl-client.go
cd ..
```

### 2. Start Services
```bash
cd /home/jeff-mboya/Documents/propeller/examples/fl-demo
docker compose up -d
```

### 3. Verify Services
```bash
docker compose ps
curl http://localhost:7070/health
```

### 4. Run Test
```bash
python3 test-fl-local.py
```

### 5. Monitor Progress
```bash
# Coordinator (aggregation)
docker compose logs -f coordinator

# Manager (orchestration)
docker compose logs -f manager

# Proplet (training)
docker compose logs -f proplet-1
```

### 6. Check Results
```bash
docker compose exec model-server ls -la /tmp/fl-models/
docker compose exec model-server cat /tmp/fl-models/global_model_v1.json
```

### 7. Clean Up
```bash
docker compose down
```

## Common Issues

**Manager not accessible?**
```bash
docker compose restart manager
sleep 5
curl http://localhost:7070/health
```

**Proplets unhealthy?**
```bash
docker compose restart proplet-1 proplet-2 proplet-3
```

**MQTT connection issues?**
```bash
docker compose logs mqtt
docker compose restart mqtt
```

For detailed troubleshooting, see [TESTING.md](./TESTING.md).
