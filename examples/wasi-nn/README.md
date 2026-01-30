# wasi-nn Image Classification Example

Run machine learning inference using wasi-nn with OpenVINO on Propeller.

## Prerequisites

- Docker (x86_64/amd64 support required)
- Rust with `wasm32-wasip1` target
- Propeller running (see [main documentation](https://github.com/absmach/propeller))

## Quick Start

### 1. Prepare Model Files

```bash
mkdir -p fixture
cd fixture
wget https://download.01.org/openvinotoolkit/fixtures/mobilenet/mobilenet.bin -O model.bin
wget https://download.01.org/openvinotoolkit/fixtures/mobilenet/mobilenet.xml -O model.xml
wget https://download.01.org/openvinotoolkit/fixtures/mobilenet/tensor-1x224x224x3-f32.bgr -O tensor.bgr
cd ..
```

### 2. Build WASM Binary

```bash
git clone https://github.com/bytecodealliance/wasmtime.git
cd wasmtime/crates/wasi-nn/examples/classification-example

rustup target add wasm32-wasip1
cargo build --target wasm32-wasip1 --release

# Note the output path
ls target/wasm32-wasip1/release/wasi-nn-example.wasm
```

### 3. Create Task

```bash
# From propeller directory
propeller-cli tasks create wasi-nn-inference \
    --cli-args="-S,nn,--dir=/home/proplet/fixture::fixture"

# Save the task ID from response
export TASK_ID="<task-id-from-response>"
```

### 4. Upload and Run

```bash
# Upload WASM
curl -X PUT "http://localhost:7070/tasks/${TASK_ID}/upload" \
    -F "file=@path/to/wasi-nn-example.wasm"

# Start task
propeller-cli tasks start $TASK_ID

# Check logs
docker compose -f docker/compose.yaml logs proplet --tail 20
```

## Expected Output

Successful inference shows:

```
Found results, sorted top 5: [InferenceResult(885, 0.3958259), InferenceResult(904, 0.36464667), ...]
```

Top prediction: Class 885 = "quill" (ImageNet)

## Notes

- **Architecture**: x86_64 only (OpenVINO requirement)
- **Apple Silicon**: Runs via emulation automatically
- CLI args format: `-S nn` enables wasi-nn, `--dir=<host>::<guest>` mounts directories

## Full Documentation

For detailed setup, troubleshooting, and advanced usage, see [Propeller Documentation](https://github.com/absmach/propeller-docs/tree/main/docs).
