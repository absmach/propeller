package sdf

import "github.com/absmach/propeller/pkg/proplet"

const schemaVersion = "1.0"

func PropletDocument(p proplet.Proplet) Document {
	return Document{
		Info: Info{
			Title:   "Propeller Proplet: " + p.Name + " (" + p.ID + ")",
			Version: schemaVersion,
			License: "Apache-2.0",
		},
		SdfThing: map[string]ThingAfford{
			"Proplet": {
				Description: "A Propeller proplet — an edge node that executes WebAssembly tasks",
				SdfProperty: map[string]PropertyAfford{
					"id": {
						DataAfford: DataAfford{
							Description: "Unique proplet identifier",
							Type:        "string",
							ReadOnly:    true,
						},
					},
					"name": {
						DataAfford: DataAfford{
							Description: "Human-readable proplet name",
							Type:        "string",
						},
					},
					"alive": {
						DataAfford: DataAfford{
							Description: "Whether the proplet sent a heartbeat within the liveness window",
							Type:        "boolean",
							ReadOnly:    true,
						},
						Observable: true,
					},
					"task_count": {
						DataAfford: DataAfford{
							Description: "Number of tasks currently running on this proplet",
							Type:        "integer",
							ReadOnly:    true,
							Minimum:     minPtr(),
						},
						Observable: true,
					},
					"metadata": {
						DataAfford: DataAfford{
							SdfRef: "#/sdfThing/Proplet/sdfData/PropletMetadata",
						},
					},
				},
				SdfAction: map[string]ActionAfford{
					"start_task": {
						Description: "Dispatch a WebAssembly task to this proplet",
						SdfInputData: &DataAfford{
							SdfRef: "#/sdfThing/Proplet/sdfData/TaskDispatch",
						},
					},
					"stop_task": {
						Description: "Stop a running task on this proplet",
						SdfInputData: &DataAfford{
							SdfRef: "#/sdfThing/Proplet/sdfData/TaskStop",
						},
					},
				},
				SdfEvent: map[string]EventAfford{
					"heartbeat": {
						Description: "Periodic liveness signal from the proplet",
						SdfOutputData: &DataAfford{
							SdfRef: "#/sdfThing/Proplet/sdfData/HeartbeatPayload",
						},
					},
					"task_result": {
						Description: "Emitted when a task finishes (success or failure)",
						SdfOutputData: &DataAfford{
							SdfRef: "#/sdfThing/Proplet/sdfData/TaskResult",
						},
					},
				},
				SdfData: map[string]DataAfford{
					"PropletMetadata": {
						Description: "Static metadata reported by the proplet at registration",
						Type:        "object",
						Properties: map[string]DataAfford{
							"description":        {Type: "string"},
							"tags":               {Type: "array", Items: &DataAfford{Type: "string"}},
							"location":           {Type: "string"},
							"ip":                 {Type: "string"},
							"environment":        {Type: "string"},
							"os":                 {Type: "string"},
							"hostname":           {Type: "string"},
							"cpu_arch":           {Type: "string"},
							"total_memory_bytes": {Type: "integer", Minimum: minPtr()},
							"proplet_version":    {Type: "string"},
							"wasm_runtime":       {Type: "string"},
						},
					},
					"TaskDispatch": {
						Description: "Payload used to dispatch a task to the proplet",
						Type:        "object",
						Required:    []string{"id", "name"},
						Properties: map[string]DataAfford{
							"id":                {Type: "string"},
							"name":              {Type: "string"},
							"image_url":         {Type: "string"},
							"cli_args":          {Type: "array", Items: &DataAfford{Type: "string"}},
							"inputs":            {Type: "array", Items: &DataAfford{Type: "string"}},
							"env":               {Type: "object", AdditionalProperties: &DataAfford{Type: "string"}},
							"daemon":            {Type: "boolean"},
							"encrypted":         {Type: "boolean"},
							"kbs_resource_path": {Type: "string"},
						},
					},
					"TaskStop": {
						Description: "Payload used to stop a running task on the proplet",
						Type:        "object",
						Required:    []string{"id"},
						Properties: map[string]DataAfford{
							"id": {Type: "string", Description: "Identifier of the task to stop"},
						},
					},
					"HeartbeatPayload": {
						Description: "Payload of a proplet heartbeat event",
						Type:        "object",
						Properties: map[string]DataAfford{
							"proplet_id": {Type: "string"},
							"name":       {Type: "string"},
							"task_count": {Type: "integer", Minimum: minPtr()},
							"alive":      {Type: "boolean"},
						},
					},
					"TaskResult": {
						Description: "Result payload emitted after a task completes",
						Type:        "object",
						Properties: map[string]DataAfford{
							"task_id":    {Type: "string"},
							"proplet_id": {Type: "string"},
							"state":      {Type: "string", Enum: []any{"Completed", "Failed", "Skipped", "Interrupted"}},
							"results": {
								Type:        "object",
								Description: "Key-value output produced by the task; keys and value types depend on the wasm module",
							},
							"error": {Type: "string"},
						},
					},
				},
			},
		},
	}
}

func minPtr() *float64 {
	v := float64(0)

	return &v
}
