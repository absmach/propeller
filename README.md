# 🚀 Propeller

**Propeller** is a cutting-edge orchestrator for **WebAssembly (Wasm)** workloads across the **Cloud-Edge continuum**. It enables seamless deployment of Wasm applications from powerful cloud servers to constrained microcontrollers, combining flexibility, security, and performance.

---

## 🌟 Features

- 🌐 **Cloud-Edge Orchestration**: Deploy Wasm workloads effortlessly across diverse environments, from robust cloud servers to lightweight microcontrollers.
- ⚡ **Fast Boot Times**: Take advantage of Wasm's near-instant startup for efficient workload execution.
- 📦 **FaaS Deployment**: Enable Function-as-a-Service (FaaS) capabilities for scalable and event-driven applications.
- 🖥️ **OCI Registry Support**: Push and pull Wasm workloads from OCI-compliant registries for streamlined workflow integration.
- 🔧 **WAMR on Zephyr RTOS**: Deploy lightweight Wasm workloads on constrained devices running Zephyr RTOS via the WebAssembly Micro Runtime (WAMR).
- 🛠️ **Powerful Service Mesh**: Integrates with **[SuperMQ](https://github.com/absmach)** for secure, efficient IoT device communication.
- 🔒 **Security at the Core**: Propeller ensures secure workload execution and communication for IoT environments.

---

## 🏗️ Architecture Overview

Propeller's architecture consists of three key components:

### 1. 🖥️ **User Interface (CLI/API)**

- Provides users with tools to interact with Propeller, whether via a command-line interface (CLI) or RESTful API.

### 2. 🚀 **Manager**

- Acts as the control hub, responsible for workload scheduling and orchestration.
- Integrates a **scheduler** for efficient resource allocation and workload distribution.
- Maintains an internal database for tracking workloads, worker states, and metadata.
- Currently, the system supports **1 manager : multiple workers** as shown in (a). In the future, the system will be expanded to support **multiple managers : multiple workers** as shown in (b).

### 3. ⚙️ **Workers**

- Responsible for executing workloads based on instructions from the manager.
- All workers operate within the same communication channel.
- Two worker types are supported:
  - **Golang Workers**: Designed for general-purpose workloads on cloud or edge devices.
  - **C & Rust Workers**: Optimized for constrained microcontroller environments, enabling lightweight and efficient task execution.
- Workers communicate using multiple protocols:
  - MQTT and CoAP for constrained devices.
  - WebSocket (WS) for other devices.
- At present, the system is configured to support a **1 worker : 1 task** execution model as shown in (a). In the future, the system will be expanded to support **1 worker : multiple tasks** as shown in (b).

![Propeller Orchestration Diagram](architecture.svg)

---

## 🛠️ How It Works

1. **Develop in WebAssembly**: Write portable, lightweight Wasm workloads for your application.
2. **Register Workloads**: Push your workloads to an OCI-compliant registry for easy deployment.
3. **Deploy Anywhere**: Use Propeller to orchestrate and manage workload deployment across the cloud, edge, and IoT devices.
4. **Monitor & Scale**: Leverage real-time monitoring and dynamic scaling to optimize your system's performance.

---

## 📖 Documentation

For setup instructions, API references, and usage examples, see the documentation:  
🔗 [Documentation Link](#)

---

## 💡 Use Cases

- 🏭 **Industrial IoT**: Deploy analytics or control applications to edge devices in factories.
- 🛡️ **Secure Workloads**: Run isolated, portable workloads securely on cloud or edge devices.
- 🌎 **Smart Cities**: Power scalable IoT networks with efficient communication and dynamic workloads.
- ☁️ **Serverless Applications**: Deploy FaaS applications leveraging Propeller's Wasm orchestration capabilities.

---

## 🤝 Contributing

Contributions are welcome! Please check the [CONTRIBUTING.md](#) for details on how to get started.

---

## 📜 License

Propeller is licensed under the **Apache-2.0 License**. See the [LICENSE](LICENSE) file for more details.
