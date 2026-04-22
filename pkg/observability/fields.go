package observability

// Standard field names for structured logging across all Propeller services.
// Using consistent field names enables better log aggregation and querying.

// FieldTaskID is the task identifier field.
const FieldTaskID = "task_id"

// FieldPropletID is the proplet identifier field.
const FieldPropletID = "proplet_id"

// FieldJobID is the job identifier field.
const FieldJobID = "job_id"

// FieldWorkflowID is the workflow identifier field.
const FieldWorkflowID = "workflow_id"

// FieldRoundID is the FL round identifier field.
const FieldRoundID = "round_id"

// FieldAppName is the application name field.
const FieldAppName = "app_name"

// FieldTraceID is the trace identifier field for distributed tracing.
const FieldTraceID = "trace_id"

// FieldSpanID is the span identifier field for distributed tracing.
const FieldSpanID = "span_id"

// FieldDuration is the duration field in milliseconds.
const FieldDuration = "duration_ms"

// FieldTimestamp is the timestamp field.
const FieldTimestamp = "timestamp"

// FieldStartTime is the start time field.
const FieldStartTime = "start_time"

// FieldEndTime is the end time field.
const FieldEndTime = "end_time"

// FieldUptime is the uptime field in seconds.
const FieldUptime = "uptime_seconds"

// FieldError is the error message field.
const FieldError = "error"

// FieldErrorType is the error type/category field.
const FieldErrorType = "error_type"

// FieldStatus is the status field.
const FieldStatus = "status"

// FieldState is the state field.
const FieldState = "state"

// FieldCount is the count field.
const FieldCount = "count"

// FieldOffset is the pagination offset field.
const FieldOffset = "offset"

// FieldLimit is the pagination limit field.
const FieldLimit = "limit"

// FieldTotal is the total count field.
const FieldTotal = "total"

// FieldSize is the size in bytes field.
const FieldSize = "size_bytes"

// FieldChunkNum is the chunk number field.
const FieldChunkNum = "chunk_num"

// FieldTotalChunks is the total chunks field.
const FieldTotalChunks = "total_chunks"

// FieldComponent is the component name field.
const FieldComponent = "component"

// FieldService is the service name field.
const FieldService = "service"

// FieldMethod is the method name field.
const FieldMethod = "method"

// FieldOperation is the operation name field.
const FieldOperation = "operation"

// FieldTopic is the MQTT topic field.
const FieldTopic = "topic"

// FieldQoS is the MQTT QoS field.
const FieldQoS = "qos"

// FieldPayload is the payload size field.
const FieldPayload = "payload_size"

// FieldCPUPercent is the CPU usage percentage field.
const FieldCPUPercent = "cpu_percent"

// FieldMemoryBytes is the memory usage in bytes field.
const FieldMemoryBytes = "memory_bytes"

// FieldMemoryPercent is the memory usage percentage field.
const FieldMemoryPercent = "memory_percent"

// FieldDiskRead is the disk read bytes field.
const FieldDiskRead = "disk_read_bytes"

// FieldDiskWrite is the disk write bytes field.
const FieldDiskWrite = "disk_write_bytes"

// FieldThreadCount is the thread count field.
const FieldThreadCount = "thread_count"

// FieldTaskKind is the task kind/type field.
const FieldTaskKind = "task_kind"

// FieldExecutionMode is the execution mode field.
const FieldExecutionMode = "execution_mode"

// FieldWasmRuntime is the WASM runtime field.
const FieldWasmRuntime = "wasm_runtime"

// FieldWasmModuleSize is the WASM module size field.
const FieldWasmModuleSize = "wasm_module_size"

// FieldRemoteAddr is the remote address field.
const FieldRemoteAddr = "remote_addr"

// FieldHTTPMethod is the HTTP method field.
const FieldHTTPMethod = "http_method"

// FieldHTTPPath is the HTTP path field.
const FieldHTTPPath = "http_path"

// FieldHTTPStatus is the HTTP status code field.
const FieldHTTPStatus = "http_status"

// Component names for consistent service identification.
const (
	// ComponentManager is the manager service.
	ComponentManager = "manager"
	// ComponentProxy is the proxy service.
	ComponentProxy = "proxy"
	// ComponentProplet is the proplet service.
	ComponentProplet = "proplet"
	// ComponentScheduler is the scheduler component.
	ComponentScheduler = "scheduler"
	// ComponentStorage is the storage component.
	ComponentStorage = "storage"
	// ComponentMQTT is the MQTT component.
	ComponentMQTT = "mqtt"
	// ComponentHTTP is the HTTP component.
	ComponentHTTP = "http"
	// ComponentWorkflow is the workflow component.
	ComponentWorkflow = "workflow"
	// ComponentFL is the federated learning component.
	ComponentFL = "fl"
	// ComponentCron is the cron scheduler component.
	ComponentCron = "cron"
)

// Operation names for tracing spans.
const (
	// OpCreateTask is the create task operation.
	OpCreateTask = "create-task"
	// OpStartTask is the start task operation.
	OpStartTask = "start-task"
	// OpStopTask is the stop task operation.
	OpStopTask = "stop-task"
	// OpGetTask is the get task operation.
	OpGetTask = "get-task"
	// OpListTasks is the list tasks operation.
	OpListTasks = "list-tasks"
	// OpUpdateTask is the update task operation.
	OpUpdateTask = "update-task"
	// OpDeleteTask is the delete task operation.
	OpDeleteTask = "delete-task"
	// OpGetTaskResults is the get task results operation.
	OpGetTaskResults = "get-task-results"
	// OpGetTaskMetrics is the get task metrics operation.
	OpGetTaskMetrics = "get-task-metrics"

	// OpCreateProplet is the create proplet operation.
	OpCreateProplet = "create-proplet"
	// OpGetProplet is the get proplet operation.
	OpGetProplet = "get-proplet"
	// OpListProplets is the list proplets operation.
	OpListProplets = "list-proplets"
	// OpDeleteProplet is the delete proplet operation.
	OpDeleteProplet = "delete-proplet"
	// OpSelectProplet is the select proplet operation.
	OpSelectProplet = "select-proplet"
	// OpGetPropletMetrics is the get proplet metrics operation.
	OpGetPropletMetrics = "get-proplet-metrics"

	// OpCreateJob is the create job operation.
	OpCreateJob = "create-job"
	// OpStartJob is the start job operation.
	OpStartJob = "start-job"
	// OpStopJob is the stop job operation.
	OpStopJob = "stop-job"
	// OpGetJob is the get job operation.
	OpGetJob = "get-job"
	// OpListJobs is the list jobs operation.
	OpListJobs = "list-jobs"

	// OpCreateWorkflow is the create workflow operation.
	OpCreateWorkflow = "create-workflow"

	// OpConfigureExperiment is the configure FL experiment operation.
	OpConfigureExperiment = "configure-experiment"
	// OpGetFLTask is the get FL task operation.
	OpGetFLTask = "get-fl-task"
	// OpPostFLUpdate is the post FL update operation.
	OpPostFLUpdate = "post-fl-update"
	// OpGetRoundStatus is the get round status operation.
	OpGetRoundStatus = "get-round-status"

	// OpFetchContainer is the fetch container operation.
	OpFetchContainer = "fetch-container"
	// OpSendChunk is the send chunk operation.
	OpSendChunk = "send-chunk"

	// OpSubscribe is the MQTT subscribe operation.
	OpSubscribe = "subscribe"
	// OpPublish is the MQTT publish operation.
	OpPublish = "publish"
	// OpHandleMessage is the handle message operation.
	OpHandleMessage = "handle-message"

	// OpExecuteWasm is the execute WASM operation.
	OpExecuteWasm = "execute-wasm"
	// OpLoadModule is the load module operation.
	OpLoadModule = "load-module"
)

// MQTT topic type labels for metrics.
const (
	// TopicTypeControl is the control topic type.
	TopicTypeControl = "control"
	// TopicTypeResults is the results topic type.
	TopicTypeResults = "results"
	// TopicTypeMetrics is the metrics topic type.
	TopicTypeMetrics = "metrics"
	// TopicTypeDiscovery is the discovery topic type.
	TopicTypeDiscovery = "discovery"
	// TopicTypeAlive is the alive/heartbeat topic type.
	TopicTypeAlive = "alive"
	// TopicTypeRegistry is the registry topic type.
	TopicTypeRegistry = "registry"
	// TopicTypeFL is the federated learning topic type.
	TopicTypeFL = "fl"
)
