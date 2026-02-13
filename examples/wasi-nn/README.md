# wasi-nn with OpenVINO on Propeller

Run machine learning inference at the edge using [wasi-nn](https://github.com/WebAssembly/wasi-nn) with the [OpenVINO](https://docs.openvino.ai/) backend via wasmtime.

## Prerequisites

- x86_64/amd64 host (OpenVINO requirement)
- Docker
- The `proplet:wasi-nn` image built (`make docker_proplet_wasinn`)
- Rust toolchain with `wasm32-wasip1` target (`rustup target add wasm32-wasip1`)

## Quick start: MobileNet classification example

This walkthrough uses the [wasmtime wasi-nn classification example](https://github.com/bytecodealliance/wasmtime/tree/main/crates/wasi-nn/examples/classification-example) with the MobileNet model.

### 1. Build the WASM module

```bash
mkdir -p /tmp/wasi-nn-example/src /tmp/wasi-nn-example/fixture
```

Create `/tmp/wasi-nn-example/Cargo.toml`:

```toml
[package]
name = "wasi-nn-example"
version = "0.0.0"
edition = "2021"
publish = false

[dependencies]
wasi-nn = "0.1.0"

[workspace]
```

Create `/tmp/wasi-nn-example/src/main.rs`:

```rust
use std::convert::TryInto;
use std::fs;

pub fn main() {
    let xml = fs::read_to_string("fixture/model.xml").unwrap();
    println!("Read graph XML, first 50 characters: {}", &xml[..50]);

    let weights = fs::read("fixture/model.bin").unwrap();
    println!("Read graph weights, size in bytes: {}", weights.len());

    let graph = unsafe {
        wasi_nn::load(
            &[&xml.into_bytes(), &weights],
            wasi_nn::GRAPH_ENCODING_OPENVINO,
            wasi_nn::EXECUTION_TARGET_CPU,
        )
        .unwrap()
    };
    println!("Loaded graph into wasi-nn with ID: {graph}");

    let context = unsafe { wasi_nn::init_execution_context(graph).unwrap() };
    println!("Created wasi-nn execution context with ID: {context}");

    let tensor_data = fs::read("fixture/tensor.bgr").unwrap();
    println!("Read input tensor, size in bytes: {}", tensor_data.len());
    let tensor = wasi_nn::Tensor {
        dimensions: &[1, 3, 224, 224],
        r#type: wasi_nn::TENSOR_TYPE_F32,
        data: &tensor_data,
    };
    unsafe {
        wasi_nn::set_input(context, 0, tensor).unwrap();
    }

    unsafe {
        wasi_nn::compute(context).unwrap();
    }
    println!("Executed graph inference");

    let mut output_buffer = vec![0f32; 1001];
    unsafe {
        wasi_nn::get_output(
            context,
            0,
            &mut output_buffer[..] as *mut [f32] as *mut u8,
            (output_buffer.len() * 4).try_into().unwrap(),
        )
        .unwrap();
    }
    println!(
        "Found results, sorted top 5: {:?}",
        &sort_results(&output_buffer)[..5]
    )
}

fn sort_results(buffer: &[f32]) -> Vec<InferenceResult> {
    let mut results: Vec<InferenceResult> = buffer
        .iter()
        .skip(1)
        .enumerate()
        .map(|(c, p)| InferenceResult(c, *p))
        .collect();
    results.sort_by(|a, b| b.1.partial_cmp(&a.1).unwrap());
    results
}

#[derive(Debug, PartialEq)]
struct InferenceResult(usize, f32);
```

Compile to WASM:

```bash
cd /tmp/wasi-nn-example
cargo build --target=wasm32-wasip1
```

Output:

```text
   Compiling wasi-nn v0.1.0
   Compiling wasi-nn-example v0.0.0 (/tmp/wasi-nn-example)
    Finished `dev` profile [unoptimized + debuginfo] target(s) in 4.21s
```

### 2. Download MobileNet model fixtures

```bash
wget -q https://download.01.org/openvinotoolkit/fixtures/mobilenet/mobilenet.xml -O /tmp/wasi-nn-example/fixture/model.xml
wget -q https://download.01.org/openvinotoolkit/fixtures/mobilenet/mobilenet.bin -O /tmp/wasi-nn-example/fixture/model.bin
wget -q https://download.01.org/openvinotoolkit/fixtures/mobilenet/tensor-1x224x224x3-f32.bgr -O /tmp/wasi-nn-example/fixture/tensor.bgr
```

Verify:

```bash
ls -lh /tmp/wasi-nn-example/fixture/
```

Output:

```text
total 15M
-rw-rw-r-- 1 user user  14M Oct  3  2024 model.bin
-rw-rw-r-- 1 user user 141K Jul  3  2024 model.xml
-rw-rw-r-- 1 user user 588K Jul  3  2024 tensor.bgr
```

### 3. Build the proplet wasi-nn image

```bash
make docker_proplet_wasinn
```

Output (last lines):

```text
#18 naming to ghcr.io/absmach/propeller/proplet:wasi-nn done
#18 DONE 0.9s
```

Verify wasmtime and OpenVINO are present:

```bash
docker run --rm --entrypoint wasmtime ghcr.io/absmach/propeller/proplet:wasi-nn --version
```

Output:

```text
wasmtime 39.0.1 (016380487 2025-11-24)
```

```bash
docker run --rm --entrypoint ls ghcr.io/absmach/propeller/proplet:wasi-nn /opt/intel/openvino_2025/runtime/lib/intel64/ | head -5
```

Output:

```text
cache.json
libopenvino.so
libopenvino.so.2025.4.0
libopenvino.so.2540
libopenvino_auto_batch_plugin.so
```

### 4. Run inference in the container

```bash
docker run --rm --entrypoint wasmtime \
  -v /tmp/wasi-nn-example/target/wasm32-wasip1/debug/wasi-nn-example.wasm:/test/wasi-nn-example.wasm \
  -v /tmp/wasi-nn-example/fixture:/home/proplet/fixture \
  ghcr.io/absmach/propeller/proplet:wasi-nn \
  run -S nn --dir=/home/proplet/fixture::fixture /test/wasi-nn-example.wasm
```

Output:

```text
Read graph XML, first 50 characters: <?xml version="1.0" ?>
<net name="mobilenet_v2_1.0
Read graph weights, size in bytes: 13956476
Loaded graph into wasi-nn with ID: 0
Created wasi-nn execution context with ID: 0
Read input tensor, size in bytes: 602112
Executed graph inference
Found results, sorted top 5: [InferenceResult(885, 0.3958259), InferenceResult(904, 0.36464667), InferenceResult(84, 0.010480272), InferenceResult(911, 0.0082290545), InferenceResult(741, 0.0072448305)]
```

The top result (class 885) corresponds to "vending machine" in ImageNet — the expected classification for the bundled test tensor.

## Running via Propeller (docker compose)

This section walks through the full Propeller-mediated path: CLI → Manager HTTP API → MQTT → Proplet → wasmtime → WASM module.

### 1. Build all Docker images and CLI

```bash
make dockers              # builds manager and proxy images
make docker_proplet_wasinn # builds proplet:wasi-nn image
make cli                  # builds propeller-cli at build/cli
```

### 2. Prepare the fixture directory

From the project root:

```bash
mkdir -p fixture
wget -q https://download.01.org/openvinotoolkit/fixtures/mobilenet/mobilenet.xml -O fixture/model.xml
wget -q https://download.01.org/openvinotoolkit/fixtures/mobilenet/mobilenet.bin -O fixture/model.bin
wget -q https://download.01.org/openvinotoolkit/fixtures/mobilenet/tensor-1x224x224x3-f32.bgr -O fixture/tensor.bgr
```

### 3. Configure docker compose

In `docker/.env`, set:

```env
PROPLET_IMAGE_TAG=wasi-nn
PROPLET_EXTERNAL_WASM_RUNTIME=wasmtime
```

In `docker/compose.yaml`, uncomment under the `proplet` service:

```yaml
platform: linux/amd64
volumes:
  - ../config.toml:/config.toml
  - ../fixture:/home/proplet/fixture
```

And uncomment under the `manager` service:

```yaml
volumes:
  - ../config.toml:/config.toml
```

And uncomment under the `proxy` service:

```yaml
volumes:
  - ../config.toml:/config.toml
```

### 4. Start the full stack

```bash
make start-supermq
```

Wait for all services to come up:

```bash
docker compose -f docker/compose.yaml ps
```

### 5. Provision via CLI

Run the interactive provisioning wizard:

```bash
./build/cli provision
```

This creates a domain, clients (manager + proplet), a channel, connects them, and generates `config.toml` in the project root.

### 6. Restart Propeller services

After provisioning generates `config.toml`, restart so the services pick up the credentials:

```bash
docker compose -f docker/compose.yaml --env-file docker/.env restart manager proplet proxy
```

Verify the proplet is alive:

```bash
curl -s http://localhost:7070/proplets | python3 -m json.tool
```

Output:

```json
{
    "total": 1,
    "proplets": [
        {
            "id": "7ab8ef0b-...",
            "name": "...",
            "task_count": 0,
            "alive": true
        }
    ]
}
```

### 7. Create, upload, and start a task

Create a task named `_start` (uses the default WASI entry point):

```bash
./build/cli tasks create _start \
  --cli-args="-S,nn,--dir=/home/proplet/fixture::fixture" \
  -m http://localhost:7070
```

Output:

```json
{
  "cli_args": ["-S", "nn", "--dir=/home/proplet/fixture::fixture"],
  "id": "6d604c9d-dc98-4dc7-b99b-7cb09fa5067a",
  "name": "_start"
}
```

Upload the WASM binary:

```bash
curl -X PUT http://localhost:7070/tasks/<task-id>/upload \
  -F "file=@/tmp/wasi-nn-example/target/wasm32-wasip1/debug/wasi-nn-example.wasm"
```

Start the task:

```bash
./build/cli tasks start <task-id> -m http://localhost:7070
```

Output:

```text
ok
```

### 8. Verify execution

Check the proplet container logs:

```bash
docker logs propeller-proplet 2>&1 | grep -A15 "completed successfully"
```

Output:

```text
Task 6d604c9d-... completed successfully. Result: Read graph XML, first 50 characters: <?xml version="1.0" ?>
<net name="mobilenet_v2_1.0
Read graph weights, size in bytes: 13956476
Loaded graph into wasi-nn with ID: 0
Created wasi-nn execution context with ID: 0
Read input tensor, size in bytes: 602112
Executed graph inference
Found results, sorted top 5: [InferenceResult(885, 0.3958259), InferenceResult(904, 0.36464667), InferenceResult(84, 0.010480272), InferenceResult(911, 0.0082290545), InferenceResult(741, 0.0072448305)]
```

The output matches the standalone test — class 885 ("vending machine") is the top classification.

> **Note:** The task name `_start` tells the proplet to use the default WASI entry point. If you use a different name, the proplet will try to `--invoke` a function with that name in the WASM module.

The `--cli-args` flag accepts comma-separated wasmtime arguments:

- `-S nn` enables the wasi-nn proposal
- `--dir=/home/proplet/fixture::fixture` maps the host fixture directory into the WASM guest

## How it works

1. The CLI `--cli-args` flag accepts comma-separated wasmtime arguments
2. These are stored as `cli_args` in the Task and sent to the proplet via MQTT
3. The proplet's host runtime inserts `cli_args` between `wasmtime run` and the WASM module path:

   ```text
   wasmtime run [--invoke <func>] <cli_args...> [--env K=V...] <module.wasm> [args...]
   ```

4. OpenVINO libraries are pre-installed in the `proplet:wasi-nn` image at `/opt/intel/openvino_2025/`

## References

- [wasmtime wasi-nn examples](https://github.com/bytecodealliance/wasmtime/tree/main/crates/wasi-nn/examples)
- [OpenVINO installation](https://docs.openvino.ai/2025/get-started/install-openvino.html)
- [wasi-nn specification](https://github.com/WebAssembly/wasi-nn)
