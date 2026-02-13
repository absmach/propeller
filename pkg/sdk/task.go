package sdk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const tasksEndpoint = "/tasks"

type Task struct {
	ID         string            `json:"id,omitempty"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind,omitempty"`
	State      uint8             `json:"state,omitempty"`
	Mode       string            `json:"mode,omitempty"`
	ImageURL   string            `json:"image_url,omitempty"`
	JobID      string            `json:"job_id,omitempty"`
	CLIArgs    []string          `json:"cli_args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	StartTime  time.Time         `json:"start_time"`
	FinishTime time.Time         `json:"finish_time"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	Results    any               `json:"results,omitempty"`
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

	url := sdk.managerURL + tasksEndpoint

	body, err := sdk.processRequest(http.MethodPost, url, data, http.StatusCreated)
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
	url := sdk.managerURL + tasksEndpoint + "/" + id

	body, err := sdk.processRequest(http.MethodGet, url, nil, http.StatusOK)
	if err != nil {
		return Task{}, err
	}

	var t Task
	if err := json.Unmarshal(body, &t); err != nil {
		return Task{}, err
	}

	return t, nil
}

func (sdk *propSDK) ListTasks(offset, limit uint64) (TaskPage, error) {
	queries := make([]string, 0)
	if offset > 0 {
		queries = append(queries, fmt.Sprintf("offset=%d", offset))
	}
	if limit > 0 {
		queries = append(queries, fmt.Sprintf("limit=%d", limit))
	}
	query := ""
	if len(queries) > 0 {
		query = "?" + strings.Join(queries, "&")
	}
	url := sdk.managerURL + tasksEndpoint + query

	body, err := sdk.processRequest(http.MethodGet, url, nil, http.StatusOK)
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
	url := sdk.managerURL + tasksEndpoint + "/" + task.ID

	body, err := sdk.processRequest(http.MethodPut, url, data, http.StatusOK)
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
	url := sdk.managerURL + tasksEndpoint + "/" + id

	if _, err := sdk.processRequest(http.MethodDelete, url, nil, http.StatusNoContent); err != nil {
		return err
	}

	return nil
}

func (sdk *propSDK) StartTask(id string) error {
	url := fmt.Sprintf("%s/tasks/%s/start", sdk.managerURL, id)

	if _, err := sdk.processRequest(http.MethodPost, url, nil, http.StatusOK); err != nil {
		return err
	}

	return nil
}

func (sdk *propSDK) StopTask(id string) error {
	url := fmt.Sprintf("%s/tasks/%s/stop", sdk.managerURL, id)

	if _, err := sdk.processRequest(http.MethodPost, url, nil, http.StatusOK); err != nil {
		return err
	}

	return nil
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

	url := sdk.managerURL + jobsEndpoint

	body, err := sdk.processRequest(http.MethodPost, url, data, http.StatusCreated)
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
	url := sdk.managerURL + jobsEndpoint + "/" + jobID

	body, err := sdk.processRequest(http.MethodGet, url, nil, http.StatusOK)
	if err != nil {
		return JobResponse{}, err
	}

	var jr JobResponse
	if err := json.Unmarshal(body, &jr); err != nil {
		return JobResponse{}, err
	}

	return jr, nil
}

func (sdk *propSDK) ListJobs(offset, limit uint64) (JobPage, error) {
	queries := make([]string, 0)
	if offset > 0 {
		queries = append(queries, fmt.Sprintf("offset=%d", offset))
	}
	if limit > 0 {
		queries = append(queries, fmt.Sprintf("limit=%d", limit))
	}
	query := ""
	if len(queries) > 0 {
		query = "?" + strings.Join(queries, "&")
	}
	url := sdk.managerURL + jobsEndpoint + query

	body, err := sdk.processRequest(http.MethodGet, url, nil, http.StatusOK)
	if err != nil {
		return JobPage{}, err
	}

	var jp JobPage
	if err := json.Unmarshal(body, &jp); err != nil {
		return JobPage{}, err
	}

	return jp, nil
}

func (sdk *propSDK) StartJob(jobID string) error {
	url := fmt.Sprintf("%s/jobs/%s/start", sdk.managerURL, jobID)

	if _, err := sdk.processRequest(http.MethodPost, url, nil, http.StatusOK); err != nil {
		return err
	}

	return nil
}

func (sdk *propSDK) StopJob(jobID string) error {
	url := fmt.Sprintf("%s/jobs/%s/stop", sdk.managerURL, jobID)

	if _, err := sdk.processRequest(http.MethodPost, url, nil, http.StatusOK); err != nil {
		return err
	}

	return nil
}
