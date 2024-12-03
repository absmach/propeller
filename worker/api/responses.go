package api

import "time"

type WorkerResponseDTO struct {
	WorkerID      string    `json:"worker_id"`
	Name          string    `json:"name"`
	State         string    `json:"state"`
	Function      string    `json:"function"`
	RestartPolicy string    `json:"restart_policy"`
	RestartCount  int       `json:"restart_count"`
	StartTime     time.Time `json:"start_time"`
	FinishTime    time.Time `json:"finish_time"`
	CPUUsage      float64   `json:"cpu_usage"`
	MemoryUsage   float64   `json:"memory_usage"`
	TaskCount     int       `json:"task_count"`
	RunningTasks  int       `json:"running_tasks"`
}
