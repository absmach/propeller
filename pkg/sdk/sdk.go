package sdk

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
)

const CTJSON string = "application/json"

type PageMetadata struct {
	Offset uint64 `json:"offset"`
	Limit  uint64 `json:"limit"`
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

	// ListTasks lists tasks.
	//
	// example:
	//  taskPage, _ := sdk.ListTasks(0, 10)
	//  fmt.Println(taskPage)
	ListTasks(offset uint64, limit uint64) (TaskPage, error)

	// UpdateTask updates a task.
	//
	// example:
	//  task := sdk.Task{
	//    Name:	 "John Doe"
	//  }
	//  task, _ := sdk.UpdateTask(task)
	//  fmt.Println(task)
	UpdateTask(task Task) (Task, error)

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

	// ListJobs lists jobs.
	//
	// example:
	//  jobPage, _ := sdk.ListJobs(0, 10)
	ListJobs(offset uint64, limit uint64) (JobPage, error)

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
