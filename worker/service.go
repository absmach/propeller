package worker

import (
	"context"
	"time"
)

type WorkerService interface {
	GetWorkerStats(ctx context.Context, workerID string) (WorkerResponseDTO, error)
}

type workerService struct{}

func NewWorkerService() WorkerService {
	return &workerService{}
}

func (s *workerService) GetWorkerStats(ctx context.Context, workerID string) (WorkerResponseDTO, error) {
	return WorkerResponseDTO{
		WorkerID:      workerID,
		Name:          "WorkerName",
		State:         "Running",
		Function:      "Compute",
		RestartPolicy: "Always",
		RestartCount:  1,
		StartTime:     time.Now().Add(-2 * time.Hour),
		FinishTime:    time.Now().Add(1 * time.Hour),
		CPUUsage:      0.75,
		MemoryUsage:   0.65,
		TaskCount:     10,
		RunningTasks:  3,
	}, nil
}
