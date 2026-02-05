package middleware

import (
	"context"
	"time"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
	"github.com/go-kit/kit/metrics"
)

var _ manager.Service = (*metricsMiddleware)(nil)

type metricsMiddleware struct {
	counter metrics.Counter
	latency metrics.Histogram
	svc     manager.Service
}

func Metrics(counter metrics.Counter, latency metrics.Histogram, svc manager.Service) manager.Service {
	return &metricsMiddleware{
		counter: counter,
		latency: latency,
		svc:     svc,
	}
}

func (mm *metricsMiddleware) GetProplet(ctx context.Context, id string) (proplet.Proplet, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-proplet").Add(1)
		mm.latency.With("method", "get-proplet").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetProplet(ctx, id)
}

func (mm *metricsMiddleware) ListProplets(ctx context.Context, offset, limit uint64) (proplet.PropletPage, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "list-proplets").Add(1)
		mm.latency.With("method", "list-proplets").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.ListProplets(ctx, offset, limit)
}

func (mm *metricsMiddleware) SelectProplet(ctx context.Context, t task.Task) (proplet.Proplet, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "select-proplet").Add(1)
		mm.latency.With("method", "select-proplet").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.SelectProplet(ctx, t)
}

func (mm *metricsMiddleware) DeleteProplet(ctx context.Context, id string) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "delete-proplet").Add(1)
		mm.latency.With("method", "delete-proplet").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.DeleteProplet(ctx, id)
}

func (mm *metricsMiddleware) CreateTask(ctx context.Context, t task.Task) (task.Task, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "create-task").Add(1)
		mm.latency.With("method", "create-task").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.CreateTask(ctx, t)
}

func (mm *metricsMiddleware) GetTask(ctx context.Context, id string) (task.Task, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-task").Add(1)
		mm.latency.With("method", "get-task").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetTask(ctx, id)
}

func (mm *metricsMiddleware) ListTasks(ctx context.Context, offset, limit uint64) (task.TaskPage, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "list-tasks").Add(1)
		mm.latency.With("method", "list-tasks").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.ListTasks(ctx, offset, limit)
}

func (mm *metricsMiddleware) UpdateTask(ctx context.Context, t task.Task) (task.Task, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "update-task").Add(1)
		mm.latency.With("method", "update-task").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.UpdateTask(ctx, t)
}

func (mm *metricsMiddleware) DeleteTask(ctx context.Context, id string) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "delete-task").Add(1)
		mm.latency.With("method", "delete-task").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.DeleteTask(ctx, id)
}

func (mm *metricsMiddleware) StartTask(ctx context.Context, id string) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "start-task").Add(1)
		mm.latency.With("method", "start-task").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.StartTask(ctx, id)
}

func (mm *metricsMiddleware) StopTask(ctx context.Context, id string) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "stop-task").Add(1)
		mm.latency.With("method", "stop-task").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.StopTask(ctx, id)
}

func (mm *metricsMiddleware) GetTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) (manager.TaskMetricsPage, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-task-metrics").Add(1)
		mm.latency.With("method", "get-task-metrics").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetTaskMetrics(ctx, taskID, offset, limit)
}

func (mm *metricsMiddleware) GetPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) (manager.PropletMetricsPage, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-proplet-metrics").Add(1)
		mm.latency.With("method", "get-proplet-metrics").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetPropletMetrics(ctx, propletID, offset, limit)
}

func (mm *metricsMiddleware) CreateWorkflow(ctx context.Context, tasks []task.Task) ([]task.Task, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "create-workflow").Add(1)
		mm.latency.With("method", "create-workflow").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.CreateWorkflow(ctx, tasks)
}

func (mm *metricsMiddleware) CreateJob(ctx context.Context, name string, tasks []task.Task, executionMode string) (string, []task.Task, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "create-job").Add(1)
		mm.latency.With("method", "create-job").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.CreateJob(ctx, name, tasks, executionMode)
}

func (mm *metricsMiddleware) GetJob(ctx context.Context, jobID string) ([]task.Task, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-job").Add(1)
		mm.latency.With("method", "get-job").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetJob(ctx, jobID)
}

func (mm *metricsMiddleware) ListJobs(ctx context.Context, offset, limit uint64) (manager.JobPage, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "list-jobs").Add(1)
		mm.latency.With("method", "list-jobs").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.ListJobs(ctx, offset, limit)
}

func (mm *metricsMiddleware) StartJob(ctx context.Context, jobID string) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "start-job").Add(1)
		mm.latency.With("method", "start-job").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.StartJob(ctx, jobID)
}

func (mm *metricsMiddleware) StopJob(ctx context.Context, jobID string) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "stop-job").Add(1)
		mm.latency.With("method", "stop-job").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.StopJob(ctx, jobID)
}

func (mm *metricsMiddleware) GetTaskResults(ctx context.Context, taskID string) (any, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-task-results").Add(1)
		mm.latency.With("method", "get-task-results").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetTaskResults(ctx, taskID)
}

func (mm *metricsMiddleware) GetParentResults(ctx context.Context, taskID string) (map[string]any, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-parent-results").Add(1)
		mm.latency.With("method", "get-parent-results").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetParentResults(ctx, taskID)
}

func (mm *metricsMiddleware) Subscribe(ctx context.Context) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "subscribe").Add(1)
		mm.latency.With("method", "subscribe").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.Subscribe(ctx)
}

func (mm *metricsMiddleware) ConfigureExperiment(ctx context.Context, config manager.ExperimentConfig) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "configure-experiment").Add(1)
		mm.latency.With("method", "configure-experiment").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.ConfigureExperiment(ctx, config)
}

func (mm *metricsMiddleware) GetFLTask(ctx context.Context, roundID, propletID string) (manager.FLTask, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-fl-task").Add(1)
		mm.latency.With("method", "get-fl-task").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetFLTask(ctx, roundID, propletID)
}

func (mm *metricsMiddleware) PostFLUpdate(ctx context.Context, update manager.FLUpdate) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "post-fl-update").Add(1)
		mm.latency.With("method", "post-fl-update").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.PostFLUpdate(ctx, update)
}

func (mm *metricsMiddleware) PostFLUpdateCBOR(ctx context.Context, updateData []byte) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "post-fl-update-cbor").Add(1)
		mm.latency.With("method", "post-fl-update-cbor").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.PostFLUpdateCBOR(ctx, updateData)
}

func (mm *metricsMiddleware) GetRoundStatus(ctx context.Context, roundID string) (manager.RoundStatus, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-round-status").Add(1)
		mm.latency.With("method", "get-round-status").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetRoundStatus(ctx, roundID)
}

func (mm *metricsMiddleware) StartCronScheduler(ctx context.Context) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "start-cron-scheduler").Add(1)
		mm.latency.With("method", "start-cron-scheduler").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.StartCronScheduler(ctx)
}
