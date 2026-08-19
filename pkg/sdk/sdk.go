package sdk

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/absmach/propeller/pkg/jsonrpc"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/sdf"
	"github.com/absmach/propeller/pkg/task"
)

const CTJSON string = "application/json"

type PageMetadata struct {
	Offset   uint64        `json:"offset"`
	Limit    uint64        `json:"limit"`
	Metadata task.Metadata `json:"metadata,omitempty"`
}

type SDK interface {
	// CreateTask creates a new task.
	//
	// example:
	//  task := sdk.Task{
	//    Name:	 "John Doe"
	//  }
	//  task, _ := sdk.CreateTask(task)
	//  fmt.Println(task)
	CreateTask(task Task) (Task, error)

	// GetTask gets a task by id.
	//
	// example:
	//  task, _ := sdk.GetTask("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040")
	//  fmt.Println(task)
	GetTask(id string) (Task, error)

	// ListTasks lists tasks with optional metadata filtering.
	//
	// example:
	//  taskPage, _ := sdk.ListTasks(sdk.PageMetadata{Offset: 0, Limit: 10})
	//  fmt.Println(taskPage)
	ListTasks(pm PageMetadata) (TaskPage, error)

	// UpdateTask updates a task.
	//
	// example:
	//  task := sdk.Task{
	//    Name:	 "John Doe"
	//  }
	//  task, _ := sdk.UpdateTask(task)
	//  fmt.Println(task)
	UpdateTask(task Task) (Task, error)

	// UploadTaskFile uploads a Wasm binary for a task via multipart form.
	//
	// example:
	//  task, _ := sdk.UploadTaskFile("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040", "/path/to/app.wasm")
	//  fmt.Println(task)
	UploadTaskFile(id string, filePath string) (Task, error)

	// GetTaskResults returns the stored execution results of a task.
	//
	// example:
	//  results, _ := sdk.GetTaskResults("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040")
	//  fmt.Println(results)
	GetTaskResults(id string) (any, error)

	// GetTaskMetrics returns the paginated metrics for a task.
	//
	// example:
	//  page, _ := sdk.GetTaskMetrics("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040", 0, 10)
	//  fmt.Println(page)
	GetTaskMetrics(id string, offset, limit uint64) (TaskMetricsPage, error)

	// DeleteTask deletes a task.
	//
	// example:
	//  task, _ := sdk.DeleteTask("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040")
	//  fmt.Println(task)
	DeleteTask(id string) error

	// StartTask starts a task.
	//
	// example:
	//  task, _ := sdk.StartTask("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040")
	//  fmt.Println(task)
	StartTask(id string) error

	// StopTask stops a task.
	//
	// example:
	//  task, _ := sdk.StopTask("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040")
	//  fmt.Println(task)
	StopTask(id string) error

	InvokeTask(id string, inputs []string, env map[string]string) (string, error)

	// Call invokes a single JSON-RPC method on the manager and returns the
	// raw result, leaving decoding to the caller.
	//
	// example:
	//  result, _ := sdk.Call("task.start", map[string]string{"id": "b1d10738"})
	//  fmt.Println(string(result))
	Call(method string, params any) (json.RawMessage, error)

	// CallBatch invokes several JSON-RPC methods in one request and returns
	// one response per call.
	//
	// example:
	//  responses, _ := sdk.CallBatch([]sdk.RPCCall{{Method: "proplet.list"}})
	//  fmt.Println(len(responses))
	CallBatch(calls []RPCCall) ([]jsonrpc.Response, error)

	// CreateJob creates a new job with multiple tasks.
	//
	// example:
	//  req := sdk.JobRequest{
	//    Name: "my-job",
	//    Tasks: []sdk.Task{...},
	//    ExecutionMode: "parallel",
	//  }
	//  job, _ := sdk.CreateJob(req)
	CreateJob(req JobRequest) (JobResponse, error)

	// GetJob gets a job by id.
	//
	// example:
	//  job, _ := sdk.GetJob("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040")
	GetJob(jobID string) (JobResponse, error)

	// ListJobs lists jobs with optional status filter.
	// Status can be "pending", "running", "completed", "failed", or "" (all).
	//
	// example:
	//  jobPage, _ := sdk.ListJobs(0, 10, "")
	//  jobPage, _ := sdk.ListJobs(0, 10, "running")
	ListJobs(offset uint64, limit uint64, status string) (JobPage, error)

	// StartJob starts a job.
	//
	// example:
	//  _ := sdk.StartJob("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040")
	StartJob(jobID string) error

	// StopJob stops a job.
	//
	// example:
	//  _ := sdk.StopJob("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040")
	StopJob(jobID string) error

	// GetProplet returns a single proplet by id.
	//
	// example:
	//  p, _ := sdk.GetProplet("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040")
	//  fmt.Println(p)
	GetProplet(id string) (Proplet, error)

	// GetPropletAliveHistory returns the paginated heartbeat history for a proplet.
	//
	// example:
	//  page, _ := sdk.GetPropletAliveHistory("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040", 0, 10)
	//  fmt.Println(page)
	GetPropletAliveHistory(id string, offset, limit uint64) (proplet.PropletAliveHistoryPage, error)

	// GetPropletMetrics returns the paginated metrics for a proplet.
	//
	// example:
	//  page, _ := sdk.GetPropletMetrics("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040", 0, 10)
	//  fmt.Println(page)
	GetPropletMetrics(id string, offset, limit uint64) (PropletMetricsPage, error)

	// ListProplets returns a paginated list of proplets, optionally filtered by status.
	//
	// example:
	//  page, _ := sdk.ListProplets(0, 10, "")
	//  fmt.Println(page)
	ListProplets(offset, limit uint64, status string) (PropletPage, error)

	// GetPropletSDF returns the SDF description of a proplet.
	//
	// example:
	//  doc, _ := sdk.GetPropletSDF("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040")
	//  fmt.Println(doc)
	GetPropletSDF(id string) (sdf.Document, error)

	// DeleteProplet deletes a proplet by id.
	//
	// example:
	//  err := sdk.DeleteProplet("b1d10738-c5d7-4ff1-8f4d-b9328ce6f040")
	//  fmt.Println(err)
	DeleteProplet(id string) error

	// CreateWorkflow creates a multi-task workflow (DAG).
	//
	// example:
	//  tasks, _ := sdk.CreateWorkflow([]sdk.Task{
	//    {Name: "step-1"},
	//    {Name: "step-2", DependsOn: []string{"<step-1-id>"}},
	//  })
	//  fmt.Println(tasks)
	CreateWorkflow(tasks []Task) ([]Task, error)

	// ConfigureExperiment configures a federated learning experiment.
	//
	// example:
	//  result, _ := sdk.ConfigureExperiment(sdk.ExperimentConfig{
	//    ExperimentID: "exp-001",
	//    RoundID: "round-1",
	//  })
	//  fmt.Println(result)
	ConfigureExperiment(config ExperimentConfig) (ExperimentResult, error)

	// GetFLTask returns the federated learning task for the current round.
	//
	// example:
	//  task, _ := sdk.GetFLTask("round-1", "proplet-1")
	//  fmt.Println(task)
	GetFLTask(roundID, propletID string) (FLTask, error)

	// PostFLUpdate submits a model update in JSON format.
	//
	// example:
	//  err := sdk.PostFLUpdate(sdk.FLUpdate{RoundID: "round-1"})
	PostFLUpdate(update FLUpdate) error

	// PostFLUpdateCBOR submits a model update in CBOR format.
	//
	// example:
	//  err := sdk.PostFLUpdateCBOR(data)
	PostFLUpdateCBOR(data []byte) error

	// GetRoundStatus returns the completion status of a federated learning round.
	//
	// example:
	//  status, _ := sdk.GetRoundStatus("round-1")
	//  fmt.Println(status)
	GetRoundStatus(roundID string) (RoundStatus, error)

	// GetHealth returns the manager health status.
	//
	// example:
	//  info, _ := sdk.GetHealth()
	//  fmt.Println(info)
	GetHealth() (HealthInfo, error)
}

type propSDK struct {
	managerURL string
	client     *http.Client
}

type Config struct {
	ManagerURL      string
	TLSVerification bool
}

func NewSDK(cfg Config) SDK {
	return &propSDK{
		managerURL: cfg.ManagerURL,
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: !cfg.TLSVerification,
				},
			},
		},
	}
}

func (sdk *propSDK) processRequest(method, reqURL string, data []byte, expectedRespCode int) ([]byte, error) {
	req, err := http.NewRequest(method, reqURL, bytes.NewReader(data))
	if err != nil {
		return []byte{}, err
	}

	req.Header.Add("Content-Type", CTJSON)

	resp, err := sdk.client.Do(req)
	if err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []byte{}, err
	}

	if resp.StatusCode != expectedRespCode {
		return []byte{}, fmt.Errorf("unexpected response code: %d", resp.StatusCode)
	}

	return body, nil
}
