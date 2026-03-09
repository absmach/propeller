# ESP32-S3 WASM Stress Benchmark

Measures the maximum number of concurrent WebAssembly workloads that can
run on an ESP32-S3-WROOM-1 using WAMR (WebAssembly Micro Runtime).

---

## 1. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│  ESP32-S3  (Xtensa LX7 dual-core @ 240 MHz, 512 KB SRAM)        │
│                                                                  │
│  app_main                                                        │
│    └─ benchmark_task (core 0, 16 KB stack)                       │
│         │                                                        │
│         ├─ wasm_runtime_full_init()  ← system allocator         │
│         │                                                        │
│         ├─ wasm_runtime_load()       ← shared wasm_module_t     │
│         │                                                        │
│         └─ bench_run() ── scaling loop                          │
│              │                                                   │
│              ├─ spawn_instance(0)                                │
│              │    ├─ wasm_runtime_instantiate()  (per instance)  │
│              │    └─ xTaskCreate → wasm_worker_task             │
│              │         └─ wasm_runtime_call_wasm("main") loop   │
│              │                                                   │
│              ├─ spawn_instance(1) ...                            │
│              │                                                   │
│              └─ metrics_sample() every step                      │
│                   ├─ heap_caps_get_free_size()                   │
│                   ├─ heap_caps_get_minimum_free_size()           │
│                   └─ FreeRTOS idle-task run-time counters        │
└─────────────────────────────────────────────────────────────────┘
```

**Memory model per instance (shared module):**

```
  wasm_module_t  (loaded once, ~2–8 KB depending on WASM size)
       │
       ├── instance 0: wasm_module_inst_t + linear memory + WAMR stack
       ├── instance 1: wasm_module_inst_t + linear memory + WAMR stack
       └── ...
              └── each instance: wasm_heap_kb + wasm_stack_kb + ~8 KB overhead
```

**CPU measurement:** FreeRTOS run-time stats via `CONFIG_FREERTOS_GENERATE_RUN_TIME_STATS`.
The idle-task counter on each core is sampled between reporting intervals.
`cpu% = 100 - (idle_delta / elapsed / 2_cores * 100)`.

---

## 2. Project Structure

```
esp32s3-wasm-stress/
├── CMakeLists.txt          Top-level; adds WAMR as extra component
├── sdkconfig.defaults      ESP32-S3 tunings + WAMR Kconfig
├── partitions.csv          3.5 MB app partition (fits WAMR + code)
├── main/
│   ├── CMakeLists.txt      IDF component registration
│   ├── main.c              app_main; experiment selector (#define EXPERIMENT)
│   ├── bench.h / bench.c   Scaling loop; instance spawning; teardown
│   ├── metrics.h / .c      Heap + CPU utilisation sampling
│   └── wasm_bins.h         Embedded WASM bytecode arrays (auto-generated)
└── wasm/
    ├── cpu_stress.c        FNV-1a hash loop (1 M iterations)
    ├── mem_stress.c        4 KB scratchpad write/read (1000 rounds)
    ├── msg_stress.c        Ring-buffer producer/consumer (10 000 msgs)
    └── build.sh            Compile with wasi-sdk → regenerate wasm_bins.h
```

---

## 3. sdkconfig.defaults

Key settings required for this project (already present in `sdkconfig.defaults`):

```ini
CONFIG_WAMR_ENABLE_INTERP=y
CONFIG_WAMR_INTERP_FAST=y
CONFIG_WAMR_ENABLE_AOT=n
CONFIG_FREERTOS_USE_TRACE_FACILITY=y
CONFIG_FREERTOS_GENERATE_RUN_TIME_STATS=y
CONFIG_ESP_TASK_WDT_CHECK_IDLE_TASK_CPU0=n
CONFIG_ESP_TASK_WDT_CHECK_IDLE_TASK_CPU1=n
```

`CONFIG_WAMR_INTERP_FAST=y` enables WAMR's fast interpreter which modifies bytecode
in-place at load time — requires the WASM binary to be in a writable DRAM buffer
(copy from flash using `heap_caps_malloc` before calling `wasm_runtime_load`).

---

## 4. Prerequisites

### ESP-IDF v5.x

```bash
# Install if needed:
# https://docs.espressif.com/projects/esp-idf/en/stable/esp32s3/get-started/

. ~/esp/v5.3.2/esp-idf/export.sh    # adjust path to your installation
```

### WAMR submodule

This project reuses the WAMR submodule from `../embed-proplet/`:

```bash
# From propeller repo root:
git submodule update --init --recursive embed-proplet/modules/wamr/wasm-micro-runtime
```

### wasi-sdk (optional – only needed to rebuild WASM workloads)

Pre-compiled WASM bytecode is embedded in `main/wasm_bins.h`.
The project builds and runs without wasi-sdk.

To recompile workloads with wasi-sdk:
```bash
export WASI_SDK_PATH=/opt/wasi-sdk   # adjust
cd wasm && ./build.sh
```

Download wasi-sdk: https://github.com/WebAssembly/wasi-sdk/releases

---

## 5. Build & Flash

```bash
cd esp32s3-wasm-stress

# Set target
idf.py set-target esp32s3

# Build
idf.py build

# Flash (adjust port)
idf.py -p /dev/ttyUSB0 flash

# Monitor
idf.py -p /dev/ttyUSB0 monitor
```

Combined:
```bash
idf.py -p /dev/ttyUSB0 flash monitor
```

If `idf.py monitor` fails in your shell (e.g. DTR/RTS line issues), use this
pyserial script directly:

```python
import serial, time, re

s = serial.Serial('/dev/ttyUSB0', 115200, timeout=0.2)
s.setDTR(False)
s.setRTS(False)
buf = ""
while True:
    data = s.read(256).decode('utf-8', errors='replace')
    if data:
        buf += data
        lines = buf.split('\n')
        buf = lines[-1]
        for line in lines[:-1]:
            clean = re.sub(r'\x1b\[[0-9;]*m', '', line).strip()
            if clean:
                print(f"[{time.strftime('%H:%M:%S')}] {clean}")
    time.sleep(0.05)
```

The `setDTR(False)` / `setRTS(False)` calls prevent the serial adapter from
holding the ESP32 in reset after opening the port.

---

## 6. Choosing an Experiment

Edit `main/main.c`, line `#define EXPERIMENT N`:

| N | Description |
|---|-------------|
| 0 | All three workloads (cpu → mem → msg) sequentially |
| 1 | CPU stress only |
| 2 | Memory stress only |
| 3 | Message-passing stress only |
| 4 | Core-pinning comparison (core0 / core1 / distributed) |
| 5 | Cooperative yield vs busy-loop comparison |
| 6 | Diverse workload: 5 distinct tasks (add/mul/fib/checksum/popcount) scaled concurrently |

---

## 7. Tuning Memory Per Instance

In `main/main.c`:

```c
#define WASM_STACK_KB  4   // WAMR interpreter operand stack
#define WASM_HEAP_KB   8   // WASM linear memory heap
#define TASK_STACK_KB  6   // FreeRTOS native task stack
```

Estimated cost per instance ≈ `WASM_STACK_KB + WASM_HEAP_KB + TASK_STACK_KB + 8 KB overhead`.

With defaults (4+8+6+8 = 26 KB), expect ~15–20 instances in 512 KB DRAM
(WAMR itself, stacks, etc. consume ~100 KB baseline).

---

## 8. Expected Serial Output

```
========================================
  ESP32-S3 WASM Stress Benchmark
========================================
Internal DRAM:
  total free  :  412 KB
  total alloc :   98 KB
  largest blk :  312 KB
  min free    :  410 KB
PSRAM         : not detected
========================================

=== WASM Stress Benchmark ===
workload=cpu  mode=shared_module  wasm_stack=4KB  wasm_heap=8KB
task_stack=6KB  priority=1  core=-1

instances=1    heap= 386KB  min= 384KB  cpu=  4%  up=4s
  └ +instance cost: ~26KB  latency: 12340us
instances=2    heap= 360KB  min= 358KB  cpu=  8%  up=7s
  └ +instance cost: ~26KB  latency: 12450us
instances=5    heap= 282KB  min= 280KB  cpu= 20%  up=16s
  └ +instance cost: ~26KB  latency: 12800us
instances=10   heap= 152KB  min= 150KB  cpu= 40%  up=31s
  └ +instance cost: ~26KB  latency: 25600us
instances=15   heap=  22KB  min=  20KB  cpu= 62%  up=46s
  └ +instance cost: ~26KB  latency: 51200us
instances=16   OOM (pre-check: heap=22KB < min required ~34KB)

--- Peak: 15 concurrent WASM instances ---

  id  workload  iters     errors  latency_us  stk_hwm
  0   cpu       847       0       51200       1024
  1   cpu       846       0       51300       1024
  ...
```

---

## 9. Interpreting Results

| Metric | Meaning |
|--------|---------|
| `heap` | Current free internal DRAM |
| `min` | All-time heap low-water mark |
| `cpu%` | Both cores combined utilisation |
| `latency_us` | µs per `wasm_execute_main()` call |
| `stk_hwm` | FreeRTOS stack bytes remaining (high-water mark) |
| `+instance cost` | Heap delta when adding this instance |
| `OOM` | Heap too small for next instance (safe stop) |
| `SPAWN_FAIL` | `xTaskCreate` or `wasm_runtime_instantiate` failed |
| `TASK_DIED` | Worker task exited immediately (WASM exception) |

**Latency scaling:** As more instances run, each instance's latency increases
proportionally because the RTOS scheduler distributes CPU time. With N
instances and T ms execution time per workload:
`latency ≈ T × N` (cooperative, single-priority scheduling).

**OOM vs CPU-bound limit:** If `cpu%` reaches ~95% before OOM, you're
CPU-limited. Reduce iterations in the WASM workload. If OOM comes first
at low CPU%, you're memory-limited – reduce `WASM_HEAP_KB` or `WASM_STACK_KB`.

---

## 10. Experiments: PSRAM

Enable PSRAM in `sdkconfig.defaults` by uncommenting:
```
CONFIG_ESP32S3_SPIRAM_SUPPORT=y
CONFIG_SPIRAM_MODE_OCT=y
CONFIG_SPIRAM_SPEED_80M=y
CONFIG_SPIRAM_USE_CAPS_ALLOC=y
```

Then in `main.c`, change the WAMR allocator to use PSRAM for instance heaps:

```c
// Replace malloc/free with PSRAM-capable versions:
init_args.mem_alloc_option.allocator.malloc_func  =
    (void *)heap_caps_malloc_prefer;   // prefers PSRAM when available
```

With 8 MB PSRAM, expect 100+ concurrent instances (CPU-limited around ~200%).

---

## 11. Advanced Experiments

### Cooperative yield vs busy-loop

In `wasm_worker_task`, replace `taskYIELD()` with a delay to simulate
cooperative I/O workloads:
```c
vTaskDelay(pdMS_TO_TICKS(1));  // 1ms yield simulates async I/O wait
```
This dramatically increases the number of concurrent instances (now I/O-bound).

### Separate runtime per instance

Change `bench_config_t.mode = MODE_SEPARATE_RUNTIME`. This calls
`wasm_runtime_full_init()` and `wasm_runtime_destroy()` per instance —
simulating fully isolated runtimes. Expect ~3× higher memory cost per instance.

### Task stack watermark analysis

After a run, check `stk_hwm` in the instance detail table. If it's < 512 bytes,
reduce `TASK_STACK_KB` is risky. If > 3 KB, you can safely reduce it.
