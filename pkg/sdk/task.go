package sdk

import (
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

const tasksEndpoint = "/tasks"

type Task struct {
	ID       string            `json:"id,omitempty"`
	Name     string            `json:"name"`
	Kind     string            `json:"kind,omitempty"`
	State    uint8             `json:"state,omitempty"`
	Mode     string            `json:"mode,omitempty"`
	ImageURL string            `json:"image_url,omitempty"`
	JobID    string            `json:"job_id,omitempty"`
	CLIArgs  []string          `json:"cli_args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	// File is the Wasm binary. When sending, set it to the base64-encoded
	// binary (format: byte). When reading task responses the manager redacts
	// the payload (e.g. "AGFzbQEAAA<REDACTED>RpdmFsdWU="), so a plain string is
	// used instead of []byte to avoid a base64 decoding failure.
	File            string         `json:"file,omitempty"`
	Inputs          []string       `json:"inputs,omitempty"`
	Daemon          bool           `json:"daemon,omitempty"`
	Latent          bool           `json:"latent,omitempty"`
	Encrypted       bool           `json:"encrypted,omitempty"`
	KBSResourcePath string         `json:"kbs_resource_path,omitempty"`
	PropletID       string         `json:"proplet_id,omitempty"`
	DependsOn       []string       `json:"depends_on,omitempty"`
	RunIf           string         `json:"run_if,omitempty"`
	WorkflowID      string         `json:"workflow_id,omitempty"`
	Broadcast       bool           `json:"broadcast,omitempty"`
	Priority        int            `json:"priority,omitempty"`
	Schedule        string         `json:"schedule,omitempty"`
	Timezone        string         `json:"timezone,omitempty"`
	IsRecurring     bool           `json:"is_recurring,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	ExtraConfig     map[string]any `json:"extra_config,omitempty"`
	StartTime       time.Time      `json:"start_time"`
	FinishTime      time.Time      `json:"finish_time"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	Results         any            `json:"results,omitempty"`
}

type TaskPage struct {
	Offset uint64 `json:"offset"`
	Limit  uint64 `json:"limit"`
	Total  uint64 `json:"total"`
	Tasks  []Task `json:"tasks"`
}

func (sdk *propSDK) CreateTask(task Task) (Task, error) {
	data, err := json.Marshal(task)
	if err != nil {
		return Task{}, err
	}

	reqURL := sdk.managerURL + tasksEndpoint

	body, err := sdk.processRequest(http.MethodPost, reqURL, data, http.StatusCreated)
	if err != nil {
		return Task{}, err
	}

	var t Task
	if err := json.Unmarshal(body, &t); err != nil {
		return Task{}, err
	}

	return t, nil
}

func (sdk *propSDK) GetTask(id string) (Task, error) {
	reqURL := sdk.managerURL + tasksEndpoint + "/" + id

	body, err := sdk.processRequest(http.MethodGet, reqURL, nil, http.StatusOK)
	if err != nil {
		return Task{}, err
	}

	var t Task
	if err := json.Unmarshal(body, &t); err != nil {
		return Task{}, err
	}

	return t, nil
}

func (sdk *propSDK) ListTasks(pm PageMetadata) (TaskPage, error) {
	queries := make([]string, 0)
	if pm.Offset > 0 {
		queries = append(queries, fmt.Sprintf("offset=%d", pm.Offset))
	}
	if pm.Limit > 0 {
		queries = append(queries, fmt.Sprintf("limit=%d", pm.Limit))
	}
	if len(pm.Metadata) > 0 {
		b, err := json.Marshal(pm.Metadata)
		if err != nil {
			return TaskPage{}, fmt.Errorf("marshal metadata: %w", err)
		}
		queries = append(queries, "metadata="+neturl.QueryEscape(string(b)))
	}
	query := ""
	if len(queries) > 0 {
		query = "?" + strings.Join(queries, "&")
	}
	reqURL := sdk.managerURL + tasksEndpoint + query

	body, err := sdk.processRequest(http.MethodGet, reqURL, nil, http.StatusOK)
	if err != nil {
		return TaskPage{}, err
	}

	var t TaskPage
	if err := json.Unmarshal(body, &t); err != nil {
		return TaskPage{}, err
	}

	return t, nil
}

func (sdk *propSDK) UpdateTask(task Task) (Task, error) {
	data, err := json.Marshal(task)
	if err != nil {
		return Task{}, err
	}
	reqURL := sdk.managerURL + tasksEndpoint + "/" + task.ID

	body, err := sdk.processRequest(http.MethodPut, reqURL, data, http.StatusOK)
	if err != nil {
		return Task{}, err
	}

	var t Task
	if err := json.Unmarshal(body, &t); err != nil {
		return Task{}, err
	}

	return t, nil
}

func (sdk *propSDK) DeleteTask(id string) error {
	reqURL := sdk.managerURL + tasksEndpoint + "/" + id

	if _, err := sdk.processRequest(http.MethodDelete, reqURL, nil, http.StatusNoContent); err != nil {
		return err
	}

	return nil
}

func (sdk *propSDK) StartTask(id string) error {
	reqURL := fmt.Sprintf("%s/tasks/%s/start", sdk.managerURL, id)

	if _, err := sdk.processRequest(http.MethodPost, reqURL, nil, http.StatusOK); err != nil {
		return err
	}

	return nil
}

func (sdk *propSDK) GetTaskResults(id string) (any, error) {
	reqURL := sdk.managerURL + tasksEndpoint + "/" + id + "/results"

	body, err := sdk.processRequest(http.MethodGet, reqURL, nil, http.StatusOK)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Results any `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return resp.Results, nil
}

func (sdk *propSDK) StopTask(id string) error {
	reqURL := fmt.Sprintf("%s/tasks/%s/stop", sdk.managerURL, id)

	if _, err := sdk.processRequest(http.MethodPost, reqURL, nil, http.StatusOK); err != nil {
		return err
	}

	return nil
}

func (sdk *propSDK) InvokeTask(id string, inputs []string, env map[string]string) (string, error) {
	reqURL := fmt.Sprintf("%s/tasks/%s/invoke", sdk.managerURL, id)

	var data []byte
	if len(inputs) > 0 || len(env) > 0 {
		payload := make(map[string]any)
		if len(inputs) > 0 {
			payload["inputs"] = inputs
		}
		if len(env) > 0 {
			payload["env"] = env
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		data = body
	}

	body, err := sdk.processRequest(http.MethodPost, reqURL, data, http.StatusOK)
	if err != nil {
		return "", err
	}

	var resp struct {
		Results string `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}

	return resp.Results, nil
}

const jobsEndpoint = "/jobs"

type JobSummary struct {
	JobID      string    `json:"job_id"`
	Name       string    `json:"name,omitempty"`
	State      uint8     `json:"state"`
	Tasks      []Task    `json:"tasks"`
	StartTime  time.Time `json:"start_time"`
	FinishTime time.Time `json:"finish_time"`
	CreatedAt  time.Time `json:"created_at"`
}

type JobPage struct {
	Offset uint64       `json:"offset"`
	Limit  uint64       `json:"limit"`
	Total  uint64       `json:"total"`
	Jobs   []JobSummary `json:"jobs"`
}

type JobRequest struct {
	Name          string `json:"name"`
	Tasks         []Task `json:"tasks"`
	ExecutionMode string `json:"execution_mode,omitempty"`
}

type JobResponse struct {
	JobID string `json:"job_id"`
	Tasks []Task `json:"tasks"`
}

func (sdk *propSDK) CreateJob(req JobRequest) (JobResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return JobResponse{}, err
	}

	reqURL := sdk.managerURL + jobsEndpoint

	body, err := sdk.processRequest(http.MethodPost, reqURL, data, http.StatusCreated)
	if err != nil {
		return JobResponse{}, err
	}

	var jr JobResponse
	if err := json.Unmarshal(body, &jr); err != nil {
		return JobResponse{}, err
	}

	return jr, nil
}

func (sdk *propSDK) GetJob(jobID string) (JobResponse, error) {
	reqURL := sdk.managerURL + jobsEndpoint + "/" + jobID

	body, err := sdk.processRequest(http.MethodGet, reqURL, nil, http.StatusOK)
	if err != nil {
		return JobResponse{}, err
	}
	if len(body) == 0 {
		return JobResponse{JobID: jobID}, nil
	}

	var jr JobResponse
	if err := json.Unmarshal(body, &jr); err != nil {
		return JobResponse{}, err
	}

	return jr, nil
}

func (sdk *propSDK) ListJobs(offset, limit uint64, status string) (JobPage, error) {
	params := make([]string, 0)
	if offset > 0 {
		params = append(params, fmt.Sprintf("offset=%d", offset))
	}
	if limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", limit))
	}
	if status != "" {
		params = append(params, "status="+neturl.QueryEscape(status))
	}
	query := ""
	if len(params) > 0 {
		query = "?" + strings.Join(params, "&")
	}
	reqURL := sdk.managerURL + jobsEndpoint + query

	body, err := sdk.processRequest(http.MethodGet, reqURL, nil, http.StatusOK)
	if err != nil {
		return JobPage{}, err
	}
	if len(body) == 0 {
		return JobPage{
			Offset: offset,
			Limit:  limit,
			Jobs:   []JobSummary{},
		}, nil
	}

	var jp JobPage
	if err := json.Unmarshal(body, &jp); err != nil {
		return JobPage{}, err
	}

	return jp, nil
}

func (sdk *propSDK) StartJob(jobID string) error {
	reqURL := fmt.Sprintf("%s/jobs/%s/start", sdk.managerURL, jobID)

	if _, err := sdk.processRequest(http.MethodPost, reqURL, nil, http.StatusOK); err != nil {
		return err
	}

	return nil
}

func (sdk *propSDK) StopJob(jobID string) error {
	reqURL := fmt.Sprintf("%s/jobs/%s/stop", sdk.managerURL, jobID)

	if _, err := sdk.processRequest(http.MethodPost, reqURL, nil, http.StatusOK); err != nil {
		return err
	}

	return nil
}
