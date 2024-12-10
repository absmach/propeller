## **Proplet Command Handling**

### **Start Command Flow**

The start command is sent by the Manager to the Proplet on the topic `channels/$CHANNEL_ID/messages/control/manager/start`

#### 1. **Parse the Start Command**

The MQTT message payload is unmarshaled into a `StartRequest` structure containing the `AppName` and any required parameters for the application. If the payload is invalid or `AppName` is missing, an error is logged, and no further action is taken.

#### 2. **Publish a Fetch Request**

A fetch request is sent to the Registry Proxy to retrieve the WebAssembly (Wasm) binary chunks for the specified application. This request is published to the topic `channels/$CHANNEL_ID/messages/registry/proplet`.

#### 3. **Wait for Wasm Binary Chunks**

The system monitors the reception of Wasm chunks from the Registry Proxy, which are published to the topic `channels/$CHANNEL_ID/messages/registry/server` and processed by the `handleChunk` function.

#### 4. **Assemble and Validate Chunks**

Once all chunks are received, as determined by comparing the number of received chunks to the `TotalChunks` field in the chunk metadata, the chunks are assembled into a complete Wasm binary and validated to ensure integrity.

#### 5. **Deploy and Run the Application**

The assembled Wasm binary is passed to the Wazero runtime for instantiation and execution, where the specified function (e.g., `main`) in the Wasm module is invoked.

---

### **Runtime Functions: StartApp**

The `StartApp` function in `runtime.go` handles the instantiation and execution of Wasm modules. It:

1. **Validate Input Parameters**: Ensures `appName`, `wasmBinary`, and `functionName` are provided and valid. Errors are returned if any parameter is missing or invalid.
2. **Acquire Mutex Lock**: Locks the runtime to ensure thread-safe access to the `modules` map.
3. **Check for Existing App Instance**: Verifies if the app is already running. If found, an error is returned to prevent duplicate instances.
4. **Instantiate the Wasm Module**: Passes the `wasmBinary` to the Wazero runtime's `Instantiate` method to create a Wasm module.
5. **Retrieve the Exported Function**: Locates the `functionName` in the module. If the function is missing, the module is closed, and an error is returned.
6. **Store the Module in the Runtime**: Saves the instantiated module in the `modules` map for tracking running applications.
7. **Release Mutex Lock**: Unlocks the runtime after the module is added to the map.
8. **Return the Exported Function**: Returns the Wasm function for execution.

---

#### 6. **Log Success or Errors**

A success message is logged if the application starts successfully, while detailed errors are logged if any step in the process (e.g., chunk assembly, instantiation, or execution) fails.

---

### **Stop Command Flow**

The stop command is sent by the Manager to the Proplet on the topic `channels/$CHANNEL_ID/messages/control/manager/stop`

#### 1. **Parse the Stop Command**

The MQTT message payload is unmarshaled into a `StopRequest` structure containing the `AppName` of the application to stop. If the payload is invalid or `AppName` is missing, an error is logged, and no further action is taken.

#### 2. **Stop the Application**

The `StopApp` method in the Wazero runtime is invoked, which checks if the application is running, closes the corresponding Wasm module, and removes the application from the runtime's internal tracking.

---

### **Runtime Functions: StopApp**

The `StopApp` function in `runtime.go` stops and cleans up a running Wasm module. It:

1. **Validate Input Parameters**: Checks if `appName` is provided. If missing, an error is returned.
2. **Acquire Mutex Lock**: Locks the runtime to ensure thread-safe access to the `modules` map.
3. **Check for Running App**: Looks up the app in the `modules` map. If the app is not found, an error is returned.
4. **Close the Wasm Module**: Calls the module's `Close` method to release all resources associated with the app. If closing fails, an error is logged and returned.
5. **Remove the App from Runtime**: Deletes the app entry from the `modules` map to update the runtime's state.
6. **Release Mutex Lock**: Unlocks the runtime after the app has been removed from the map.

---

#### 3. **Log Success or Errors**

A success message is logged with the text `"App '<AppName>' stopped successfully."` if the application stops successfully. If the application is not running or an error occurs during the stop operation, detailed error information is logged.

---

### **Proplet Topics Overview**

#### 1. **On Creation (Startup Notification)**

- **Topic**: `channels/$MANAGER_CHANNEL_ID/messages/control/proplet/create`
- **Payload**:
  ```json
  {
    "PropletID": "{PropletID}",
    "ChanID": "{ChannelID}"
  }
  ```
- **Purpose**: Notifies the Manager when a Proplet comes online.

#### 2. **Liveliness Updates**

- **Topic**: `channels/$MANAGER_CHANNEL_ID/messages/control/proplet/alive`
- **Interval**: Every 10 seconds.
- **Payload**:
  ```json
  {
    "PropletID": "{PropletID}",
    "ChanID": "{ChannelID}"
  }
  ```
- **Purpose**: Indicates the active and healthy state of the Proplet.

#### 3. **Last Will & Testament (LWT)**

- **Topic**: `channels/$MANAGER_CHANNEL_ID/messages/control/proplet/alive`
- **Payload**:
  ```json
  {
    "status": "offline",
    "PropletID": "{PropletID}",
    "ChanID": "{ChannelID}"
  }
  ```
- **Purpose**: Automatically notifies the Manager when a Proplet goes offline, as triggered by the MQTT broker.
