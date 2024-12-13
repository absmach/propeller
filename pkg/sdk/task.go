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
	ID         string    `json:"id,omitempty"`
	Name       string    `json:"name"`
	State      uint8     `json:"state,omitempty"`
	StartTime  time.Time `json:"start_time"`
	FinishTime time.Time `json:"finish_time"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
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
