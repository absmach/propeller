# 🚀 Propeller

**Propeller** is a cutting-edge orchestrator for **WebAssembly (Wasm)** workloads across the **Cloud-Edge continuum**. It enables seamless deployment of Wasm applications from powerful cloud servers to constrained microcontrollers, combining flexibility, security, and performance.

## 🌟 Features

- 🌐 **Cloud-Edge Orchestration**: Deploy Wasm workloads effortlessly across diverse environments, from robust cloud servers to lightweight microcontrollers.
- ⚡ **Fast Boot Times**: Take advantage of Wasm's near-instant startup for efficient workload execution.
- 📦 **FaaS Deployment**: Enable Function-as-a-Service (FaaS) capabilities for scalable and event-driven applications.
- 🖥️ **OCI Registry Support**: Push and pull Wasm workloads from OCI-compliant registries for streamlined workflow integration.
- 🔧 **WAMR on Zephyr RTOS**: Deploy lightweight Wasm workloads on constrained devices running Zephyr RTOS via the WebAssembly Micro Runtime (WAMR).
- 🛠️ **Powerful Service Mesh**: Integrates with **[SuperMQ](https://github.com/absmach)** for secure, efficient IoT device communication.
- 🔒 **Security at the Core**: Propeller ensures secure workload execution and communication for IoT environments.
- 🤖 **Federated Learning**: Built-in support for federated machine learning workflows with FedAvg aggregation, enabling privacy-preserving distributed training across edge devices.

## 🛠️ How It Works

1. **Develop in WebAssembly**: Write portable, lightweight Wasm workloads for your application.
2. **Register Workloads**: Push your workloads to an OCI-compliant registry for easy deployment.
3. **Deploy Anywhere**: Use Propeller to orchestrate and manage workload deployment across the cloud, edge, and IoT devices.
4. **Monitor & Scale**: Leverage real-time monitoring and dynamic scaling to optimize your system's performance.

![Propeller Orchestration Diagram](docs/architecture.svg)

## 📖 Documentation

For setup instructions, API references, and usage examples, see the documentation:
🔗 [Documentation Link](https://docs.propeller.absmach.eu/)

## 💡 Use Cases

- 🏭 **Industrial IoT**: Deploy analytics or control applications to edge devices in factories.
- 🛡️ **Secure Workloads**: Run isolated, portable workloads securely on cloud or edge devices.
- 🌎 **Smart Cities**: Power scalable IoT networks with efficient communication and dynamic workloads.
- ☁️ **Serverless Applications**: Deploy FaaS applications leveraging Propeller's Wasm orchestration capabilities.
- 🧠 **Federated Machine Learning**: Train machine learning models across distributed edge devices without exposing raw data, perfect for privacy-sensitive applications.

## 🤖 Federated Learning

Propeller includes first-class support for Federated Learning (FL) workflows, enabling distributed machine learning training across edge devices while preserving data privacy.

### Features

- **FedAvg Aggregation**: Implements Federated Averaging algorithm for aggregating model updates
- **Round-based Training**: Supports multi-round FL training with configurable rounds and clients per round
- **Model Distribution**: Automatically distributes global models to edge proplets via OCI registry
- **Privacy-Preserving**: Only model updates (weights/gradients) are shared, never raw data
- **Flexible Update Formats**: Supports multiple update formats (f32-delta, json-f64, etc.)

### Quick Start

1. **Create a Federated Learning Task**:
   ```bash
   propeller-cli fl create my-fl-task \
     --mode train \
     --image-url registry.example.com/fl-model:v1 \
     --rounds 3 \
     --clients-per-round 2 \
     --min-clients 2 \
     --update-format json-f64
   ```

2. **Start the FL Task**:
   ```bash
   propeller-cli tasks start <task-id>
   ```

3. **Monitor FL Progress**:
   ```bash
   propeller-cli fl status <task-id>
   ```

### FL Workflow

1. **Manager** creates initial round tasks and distributes them to selected proplets
2. **Proplets** train locally using private data and produce model updates
3. **Proplets** send updates (not raw data) back to the manager
4. **Manager** aggregates updates using FedAvg when minimum clients respond
5. **Manager** creates next round tasks with the aggregated global model
6. Process repeats for the specified number of rounds

### Example Wasm Module

See `examples/fl-train/fl-train.go` for a simple FL training workload example. Build it with:

```bash
GOOS=js GOARCH=wasm tinygo build -o build/fl-train.wasm -target wasip1 examples/fl-train/fl-train.go
```

### Architecture Notes

- **Rust Proplet Only**: Propeller now uses only the Rust proplet implementation (Wasmtime runtime) for executing FL workloads
- **MQTT Communication**: FL coordination uses MQTT topics under `m/{domain}/c/{channel}/control/...`
- **Chunked Transport**: Large model artifacts are automatically chunked for efficient MQTT transport

## 🤝 Contributing

Contributions are welcome! Please check the [CONTRIBUTING.md](#) for details on how to get started.

## 📜 License

Propeller is licensed under the **Apache-2.0 License**. See the [LICENSE](LICENSE) file for more details.

> 🇪🇺 This work has been partially supported by the [ELASTIC project](https://elasticproject.eu/), which received funding from the [Smart Networks and Services Joint Undertaking](https://smart-networks.europa.eu/) (SNS JU) under the European Union’s [Horizon Europe](https://research-and-innovation.ec.europa.eu/funding/funding-opportunities/funding-programmes-and-open-calls/horizon-europe_en) research and innovation programme under [Grant Agreement No. 101139067](https://cordis.europa.eu/project/id/101139067). Views and opinions expressed are however those of the author(s) only and do not necessarily reflect those of the European Union. Neither the European Union nor the granting authority can be held responsible for them.
