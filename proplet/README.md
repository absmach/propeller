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

#### 6. **Log Success or Errors**

A success message is logged if the application starts successfully, while detailed errors are logged if any step in the process (e.g., chunk assembly, instantiation, or execution) fails.

---

### **Stop Command Flow**

The stop command is sent by the Manager to the Proplet on the topic `channels/$CHANNEL_ID/messages/control/manager/stop`

#### 1. **Parse the Stop Command**

The MQTT message payload is unmarshaled into a `StopRequest` structure containing the `AppName` of the application to stop. If the payload is invalid or `AppName` is missing, an error is logged, and no further action is taken.

#### 2. **Stop the Application**

The `StopApp` method in the Wazero runtime is invoked, which checks if the application is running, closes the corresponding Wasm module, and removes the application from the runtime's internal tracking.

### **Runtime Functions: StopApp**

The `StopApp` function in `runtime.go` stops and cleans up a running Wasm module. It:

1. **Validate Input Parameters**: Checks if `appName` is provided. If missing, an error is returned.
2. **Acquire Mutex Lock**: Locks the runtime to ensure thread-safe access to the `modules` map.
3. **Check for Running App**: Looks up the app in the `modules` map. If the app is not found, an error is returned.
4. **Close the Wasm Module**: Calls the module's `Close` method to release all resources associated with the app. If closing fails, an error is logged and returned.
5. **Remove the App from Runtime**: Deletes the app entry from the `modules` map to update the runtime's state.
6. **Release Mutex Lock**: Unlocks the runtime after the app has been removed from the map.

#### 3. **Log Success or Errors**

A success message is logged with the text `"App '<AppName>' stopped successfully."` if the application stops successfully. If the application is not running or an error occurs during the stop operation, detailed error information is logged.

---

The Manager knows which Proplet is on which channel through the following mechanisms:

1. **Startup Notification (`create` topic):**

   When a Proplet starts, it publishes a message on the topic:

   ```bash
   channels/$MANAGER_CHANNEL_ID/messages/control/proplet/create
   ```

   The payload of this message includes the `PropletID` and `ChannelID`, notifying the Manager about the mapping of Proplet IDs to their respective channels:

   ```json
   {
     "PropletID": "{PropletID}",
     "ChanID": "{ChannelID}"
   }
   ```

2. **Liveliness Updates (`alive` topic):**

   To ensure that the Proplet is still active, it periodically publishes messages on the topic:

   ```bash
   channels/$MANAGER_CHANNEL_ID/messages/control/proplet/alive
   ```

   The payload contains the same `PropletID` and `ChannelID` information. This helps the Manager maintain an updated map of active Proplets and their channels:

   ```json
   {
     "status": "alive",
     "PropletID": "{PropletID}",
     "ChanID": "{ChannelID}"
   }
   ```

3. **Last Will & Testament (LWT):**

   If the Proplet goes offline unexpectedly, the MQTT broker automatically publishes a message on the same `alive` topic with a payload indicating the Proplet's offline status:

   ```json
   {
     "status": "offline",
     "PropletID": "{PropletID}",
     "ChanID": "{ChannelID}"
   }
   ```

These mechanisms ensure that the Manager is always aware of the active Proplets and their corresponding channels. The Manager can utilize this data to send specific control commands or monitor the Proplets effectively.

---

### **Registry Workflow**

1. **Proplet Fetches Wasm Binary:**

   - Publishes a fetch request on the `proplet` topic.
   - Waits for chunks on the `server` topic.

2. **Proplet Handles Registry Updates:**
   - Subscribes to the `updateRegistry` topic.
   - Updates the registry configuration upon receiving a valid payload.
   - Publishes the status (success or failure) to the `registry` topic.

#### 1. **Fetch Request**

The Proplet uses this topic to request Wasm binary chunks for a specific application from the Registry Proxy.

- **Topic**:

  ```bash
  channels/$CHANNEL_ID/messages/registry/proplet
  ```

- Payload is a JSON object containing the name of the application (`app_name`) for which the WebAssembly (Wasm) binary chunks are requested:

  ```json
  {
    "app_name": "{AppName}"
  }
  ```

#### 2. **Image Chunks Delivery**

The Registry Proxy publishes Wasm binary chunks to this topic for the Proplet to assemble into a complete binary. The Proplet monitors this topic to receive the chunks sequentially.

- **Topic**:

  ```bash
  channels/$CHANNEL_ID/messages/registry/server
  ```

- Payload is a JSON object representing a single chunk of the requested Wasm binary:

  ```json
  {
    "app_name": "{AppName}",
    "chunk_idx": {ChunkIndex},
    "total_chunks": {TotalChunks},
    "data": "{Base64EncodedChunkData}"
  }
  ```

#### 3. **Registry Configuration Update**

- Allows the Manager to update the Proplet's registry configuration dynamically.

  ```bash
  channels/$CHANNEL_ID/messages/control/manager/updateRegistry
  ```

- Payload is a JSON object containing the new registry URL and token for updating the Proplet's registry configuration:

  ```json
  {
    "registry_url": "{NewRegistryURL}",
    "registry_token": "{NewRegistryToken}"
  }
  ```

#### 4. **Acknowledgment for Registry Updates**

- The Proplet uses this topic to acknowledge whether the registry configuration update was successful or failed.

  ```bash
  channels/$CHANNEL_ID/messages/control/manager/registry
  ```

- Payload is a JSON object indicating the success or failure of a registry update:

  - Success:

    ```json
    {
      "status": "success"
    }
    ```

  - Failure:

    ```json
    {
      "status": "failure",
      "error": "{ErrorMessage}"
    }
    ```
