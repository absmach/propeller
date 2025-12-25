package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/0x6flab/namegenerator"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/task"
	"github.com/google/uuid"
)

const (
	defOffset         = 0
	defLimit          = 100
	aliveHistoryLimit = 10
)

var (
	baseTopic = "m/%s/c/%s"
	namegen   = namegenerator.NewGenerator()
)

type service struct {
	tasksDB       storage.Storage
	propletsDB    storage.Storage
	taskPropletDB storage.Storage
	metricsDB     storage.Storage
	scheduler     scheduler.Scheduler
	baseTopic     string
	pubsub        mqtt.PubSub
	logger        *slog.Logger
}

func NewService(
	tasksDB, propletsDB, taskPropletDB, metricsDB storage.Storage,
	s scheduler.Scheduler, pubsub mqtt.PubSub,
	domainID, channelID string, logger *slog.Logger,
) Service {
	return &service{
		tasksDB:       tasksDB,
		propletsDB:    propletsDB,
		taskPropletDB: taskPropletDB,
		metricsDB:     metricsDB,
		scheduler:     s,
		baseTopic:     fmt.Sprintf(baseTopic, domainID, channelID),
		pubsub:        pubsub,
		logger:        logger,
	}
}

func (svc *service) GetProplet(ctx context.Context, propletID string) (proplet.Proplet, error) {
	data, err := svc.propletsDB.Get(ctx, propletID)
	if err != nil {
		return proplet.Proplet{}, err
	}
	w, ok := data.(proplet.Proplet)
	if !ok {
		return proplet.Proplet{}, pkgerrors.ErrInvalidData
	}
	w.SetAlive()

	return w, nil
}

func (svc *service) ListProplets(ctx context.Context, offset, limit uint64) (proplet.PropletPage, error) {
	data, total, err := svc.propletsDB.List(ctx, offset, limit)
	if err != nil {
		return proplet.PropletPage{}, err
	}
	proplets := make([]proplet.Proplet, total)
	for i := range data {
		w, ok := data[i].(proplet.Proplet)
		if !ok {
			return proplet.PropletPage{}, pkgerrors.ErrInvalidData
		}
		w.SetAlive()
		proplets[i] = w
	}

	return proplet.PropletPage{
		Offset:   offset,
		Limit:    limit,
		Total:    total,
		Proplets: proplets,
	}, nil
}

func (svc *service) SelectProplet(ctx context.Context, t task.Task) (proplet.Proplet, error) {
	proplets, err := svc.ListProplets(ctx, defOffset, defLimit)
	if err != nil {
		return proplet.Proplet{}, err
	}

	return svc.scheduler.SelectProplet(t, proplets.Proplets)
}

func (svc *service) CreateTask(ctx context.Context, t task.Task) (task.Task, error) {
	t.ID = uuid.NewString()
	t.CreatedAt = time.Now()

	if err := svc.tasksDB.Create(ctx, t.ID, t); err != nil {
		return task.Task{}, err
	}

	return t, nil
}

func (svc *service) GetTask(ctx context.Context, taskID string) (task.Task, error) {
	data, err := svc.tasksDB.Get(ctx, taskID)
	if err != nil {
		return task.Task{}, err
	}
	t, ok := data.(task.Task)
	if !ok {
		return task.Task{}, pkgerrors.ErrInvalidData
	}

	return t, nil
}

func (svc *service) ListTasks(ctx context.Context, offset, limit uint64) (task.TaskPage, error) {
	data, total, err := svc.tasksDB.List(ctx, offset, limit)
	if err != nil {
		return task.TaskPage{}, err
	}

	tasks := make([]task.Task, total)
	for i := range data {
		t, ok := data[i].(task.Task)
		if !ok {
			return task.TaskPage{}, pkgerrors.ErrInvalidData
		}

		tasks[i] = t
	}

	return task.TaskPage{
		Offset: offset,
		Limit:  limit,
		Total:  total,
		Tasks:  tasks,
	}, nil
}

func (svc *service) UpdateTask(ctx context.Context, t task.Task) (task.Task, error) {
	dbT, err := svc.GetTask(ctx, t.ID)
	if err != nil {
		return task.Task{}, err
	}
	dbT.UpdatedAt = time.Now()
	if t.Name != "" {
		dbT.Name = t.Name
	}
	if t.Inputs != nil {
		dbT.Inputs = t.Inputs
	}
	if t.File != nil {
		dbT.File = t.File
	}

	if err := svc.tasksDB.Update(ctx, dbT.ID, dbT); err != nil {
		return task.Task{}, err
	}

	return dbT, nil
}

func (svc *service) DeleteTask(ctx context.Context, taskID string) error {
	return svc.tasksDB.Delete(ctx, taskID)
}

func (svc *service) StartTask(ctx context.Context, taskID string) error {
	t, err := svc.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"id":                 t.ID,
		"name":               t.Name,
		"state":              t.State,
		"image_url":          t.ImageURL,
		"file":               t.File,
		"inputs":             t.Inputs,
		"cli_args":           t.CLIArgs,
		"daemon":             t.Daemon,
		"env":                t.Env,
		"monitoring_profile": t.MonitoringProfile,
	}

	topic := svc.baseTopic + "/control/manager/start"
	if err := svc.pubsub.Publish(ctx, topic, payload); err != nil {
		return err
	}

	var p proplet.Proplet
	switch t.PropletID {
	case "":
		p, err = svc.SelectProplet(ctx, t)
		if err != nil {
			return err
		}
	default:
		p, err = svc.GetProplet(ctx, t.PropletID)
		if err != nil {
			return err
		}
		if !p.Alive {
			return fmt.Errorf("specified proplet %s is not alive", t.PropletID)
		}
	}

	if err := svc.taskPropletDB.Create(ctx, taskID, p.ID); err != nil {
		return err
	}

	p.TaskCount++
	if err := svc.propletsDB.Update(ctx, p.ID, p); err != nil {
		return err
	}

	t.State = task.Running
	t.UpdatedAt = time.Now()
	t.StartTime = time.Now()
	if err := svc.tasksDB.Update(ctx, t.ID, t); err != nil {
		return err
	}

	return nil
}

func (svc *service) StopTask(ctx context.Context, taskID string) error {
	t, err := svc.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	data, err := svc.taskPropletDB.Get(ctx, taskID)
	if err != nil {
		return err
	}
	propellerID, ok := data.(string)
	if !ok {
		return pkgerrors.ErrInvalidData
	}
	p, err := svc.GetProplet(ctx, propellerID)
	if err != nil {
		return err
	}

	topic := svc.baseTopic + "/control/manager/stop"
	if err := svc.pubsub.Publish(ctx, topic, t); err != nil {
		return err
	}

	if err := svc.taskPropletDB.Delete(ctx, taskID); err != nil {
		return err
	}

	p.TaskCount--
	if err := svc.propletsDB.Update(ctx, p.ID, p); err != nil {
		return err
	}

	return nil
}

func (svc *service) Subscribe(ctx context.Context) error {
	topic := svc.baseTopic + "/#"

	if err := svc.pubsub.Subscribe(ctx, topic, svc.handle(ctx)); err != nil {
		return err
	}

	return nil
}

func filterAndPaginateMetrics[T any](data []any, offset, limit uint64, filterFn func(any) (T, bool)) (entities []T, total uint64) {
	var filtered []T
	for _, item := range data {
		if value, ok := filterFn(item); ok {
			filtered = append(filtered, value)
		}
	}

	totalFiltered := uint64(len(filtered))

	if offset >= totalFiltered {
		return []T{}, totalFiltered
	}

	start := offset
	end := min(offset+limit, totalFiltered)

	return filtered[start:end], totalFiltered
}

func (svc *service) GetTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) (TaskMetricsPage, error) {
	data, _, err := svc.metricsDB.List(ctx, 0, 100000)
	if err != nil {
		return TaskMetricsPage{}, err
	}

	metrics, total := filterAndPaginateMetrics(data, offset, limit, func(item any) (TaskMetrics, bool) {
		if m, ok := item.(TaskMetrics); ok && m.TaskID == taskID {
			return m, true
		}

		return TaskMetrics{}, false
	})

	return TaskMetricsPage{
		Offset:  offset,
		Limit:   limit,
		Total:   total,
		Metrics: metrics,
	}, nil
}

func (svc *service) GetPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) (PropletMetricsPage, error) {
	data, _, err := svc.metricsDB.List(ctx, 0, 100000)
	if err != nil {
		return PropletMetricsPage{}, err
	}

	metrics, total := filterAndPaginateMetrics(data, offset, limit, func(item any) (PropletMetrics, bool) {
		if m, ok := item.(PropletMetrics); ok && m.PropletID == propletID {
			return m, true
		}

		return PropletMetrics{}, false
	})

	return PropletMetricsPage{
		Offset:  offset,
		Limit:   limit,
		Total:   total,
		Metrics: metrics,
	}, nil
}

func (svc *service) handle(ctx context.Context) func(topic string, msg map[string]any) error {
	return func(topic string, msg map[string]any) error {
		switch topic {
		case svc.baseTopic + "/control/proplet/create":
			if err := svc.createPropletHandler(ctx, msg); err != nil {
				return err
			}
			svc.logger.InfoContext(ctx, "successfully created proplet")
		case svc.baseTopic + "/control/proplet/alive":
			return svc.updateLivenessHandler(ctx, msg)
		case svc.baseTopic + "/control/proplet/results":
			return svc.updateResultsHandler(ctx, msg)
		case svc.baseTopic + "/control/proplet/task_metrics":
			return svc.handleTaskMetrics(ctx, msg)
		case svc.baseTopic + "/control/proplet/metrics":
			return svc.handlePropletMetrics(ctx, msg)
		}

		return nil
	}
}

func (svc *service) createPropletHandler(ctx context.Context, msg map[string]any) error {
	propletID, ok := msg["proplet_id"].(string)
	if !ok {
		return errors.New("invalid proplet_id")
	}
	if propletID == "" {
		return errors.New("proplet id is empty")
	}

	p := proplet.Proplet{
		ID:   propletID,
		Name: namegen.Generate(),
	}
	if err := svc.propletsDB.Create(ctx, p.ID, p); err != nil {
		return err
	}

	return nil
}

func (svc *service) updateLivenessHandler(ctx context.Context, msg map[string]any) error {
	propletID, ok := msg["proplet_id"].(string)
	if !ok {
		return errors.New("invalid proplet_id")
	}
	if propletID == "" {
		return errors.New("proplet id is empty")
	}

	p, err := svc.GetProplet(ctx, propletID)
	if errors.Is(err, pkgerrors.ErrNotFound) {
		return svc.createPropletHandler(ctx, msg)
	}
	if err != nil {
		return err
	}

	p.Alive = true
	p.AliveHistory = append(p.AliveHistory, time.Now())
	if len(p.AliveHistory) > aliveHistoryLimit {
		p.AliveHistory = p.AliveHistory[1:]
	}
	if err := svc.propletsDB.Update(ctx, propletID, p); err != nil {
		return err
	}

	return nil
}

func (svc *service) updateResultsHandler(ctx context.Context, msg map[string]any) error {
	taskID, ok := msg["task_id"].(string)
	if !ok {
		return errors.New("invalid task_id")
	}
	if taskID == "" {
		return errors.New("task id is empty")
	}

	t, err := svc.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	t.Results = msg["results"]
	t.State = task.Completed
	t.UpdatedAt = time.Now()
	t.FinishTime = time.Now()

	if errMsg, ok := msg["error"].(string); ok && errMsg != "" {
		t.Error = errMsg
	}

	if err := svc.tasksDB.Update(ctx, t.ID, t); err != nil {
		return err
	}

	return nil
}

func (svc *service) handleTaskMetrics(ctx context.Context, msg map[string]any) error {
	taskID, ok := msg["task_id"].(string)
	if !ok {
		return errors.New("invalid task_id")
	}
	if taskID == "" {
		return errors.New("task id is empty")
	}

	propletID, ok := msg["proplet_id"].(string)
	if !ok {
		return errors.New("invalid proplet_id")
	}

	taskMetrics := TaskMetrics{
		TaskID:    taskID,
		PropletID: propletID,
	}

	if ts, ok := msg["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			taskMetrics.Timestamp = t
		}
	}
	if taskMetrics.Timestamp.IsZero() {
		taskMetrics.Timestamp = time.Now()
	}

	if metricsData, ok := msg["metrics"].(map[string]any); ok {
		taskMetrics.Metrics = svc.parseProcessMetrics(metricsData)
	}

	if aggData, ok := msg["aggregated"].(map[string]any); ok {
		taskMetrics.Aggregated = svc.parseAggregatedMetrics(aggData)
	}

	key := fmt.Sprintf("%s:%d", taskID, taskMetrics.Timestamp.UnixNano())
	if err := svc.metricsDB.Create(ctx, key, taskMetrics); err != nil {
		svc.logger.WarnContext(ctx, "failed to store task metrics", "error", err, "task_id", taskID)

		return err
	}

	return nil
}

func (svc *service) handlePropletMetrics(ctx context.Context, msg map[string]any) error {
	propletID, ok := msg["proplet_id"].(string)
	if !ok {
		return errors.New("invalid proplet_id")
	}
	if propletID == "" {
		return errors.New("proplet id is empty")
	}
	namespace, _ := msg["namespace"].(string)

	propletMetrics := PropletMetrics{
		PropletID: propletID,
		Namespace: namespace,
	}

	if ts, ok := msg["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			propletMetrics.Timestamp = t
		}
	}
	if propletMetrics.Timestamp.IsZero() {
		propletMetrics.Timestamp = time.Now()
	}

	if cpuData, ok := msg["cpu_metrics"].(map[string]any); ok {
		propletMetrics.CPU = svc.parseCPUMetrics(cpuData)
	}

	if memData, ok := msg["memory_metrics"].(map[string]any); ok {
		propletMetrics.Memory = svc.parseMemoryMetrics(memData)
	}

	key := fmt.Sprintf("%s:%d", propletID, propletMetrics.Timestamp.UnixNano())
	if err := svc.metricsDB.Create(ctx, key, propletMetrics); err != nil {
		svc.logger.WarnContext(ctx, "failed to store proplet metrics", "error", err, "proplet_id", propletID)

		return err
	}

	return nil
}

func (svc *service) parseProcessMetrics(data map[string]any) proplet.ProcessMetrics {
	metrics := proplet.ProcessMetrics{}

	if val, ok := data["cpu_percent"].(float64); ok {
		metrics.CPUPercent = val
	}
	if val, ok := data["memory_bytes"].(float64); ok {
		metrics.MemoryBytes = uint64(val)
	}
	if val, ok := data["memory_percent"].(float64); ok {
		metrics.MemoryPercent = float32(val)
	}
	if val, ok := data["disk_read_bytes"].(float64); ok {
		metrics.DiskReadBytes = uint64(val)
	}
	if val, ok := data["disk_write_bytes"].(float64); ok {
		metrics.DiskWriteBytes = uint64(val)
	}
	if val, ok := data["uptime_seconds"].(float64); ok {
		metrics.UptimeSeconds = int64(val)
	}
	if val, ok := data["thread_count"].(float64); ok {
		metrics.ThreadCount = int32(val)
	}
	if val, ok := data["file_descriptor_count"].(float64); ok {
		metrics.FileDescriptorCount = int32(val)
	}

	return metrics
}

func (svc *service) parseAggregatedMetrics(data map[string]any) *proplet.AggregatedMetrics {
	metrics := &proplet.AggregatedMetrics{}

	if val, ok := data["avg_cpu_usage"].(float64); ok {
		metrics.AvgCPUUsage = val
	}
	if val, ok := data["max_cpu_usage"].(float64); ok {
		metrics.MaxCPUUsage = val
	}
	if val, ok := data["avg_memory_usage"].(float64); ok {
		metrics.AvgMemoryUsage = uint64(val)
	}
	if val, ok := data["max_memory_usage"].(float64); ok {
		metrics.MaxMemoryUsage = uint64(val)
	}
	if val, ok := data["total_disk_read"].(float64); ok {
		metrics.TotalDiskRead = uint64(val)
	}
	if val, ok := data["total_disk_write"].(float64); ok {
		metrics.TotalDiskWrite = uint64(val)
	}
	if val, ok := data["sample_count"].(float64); ok {
		metrics.SampleCount = int(val)
	}

	return metrics
}

func (svc *service) parseCPUMetrics(data map[string]any) proplet.CPUMetrics {
	metrics := proplet.CPUMetrics{}

	if val, ok := data["user_seconds"].(float64); ok {
		metrics.UserSeconds = val
	}
	if val, ok := data["system_seconds"].(float64); ok {
		metrics.SystemSeconds = val
	}
	if val, ok := data["percent"].(float64); ok {
		metrics.Percent = val
	}

	return metrics
}

func (svc *service) parseMemoryMetrics(data map[string]any) proplet.MemoryMetrics {
	metrics := proplet.MemoryMetrics{}

	if val, ok := data["rss_bytes"].(float64); ok {
		metrics.RSSBytes = uint64(val)
	}
	if val, ok := data["heap_alloc_bytes"].(float64); ok {
		metrics.HeapAllocBytes = uint64(val)
	}
	if val, ok := data["heap_sys_bytes"].(float64); ok {
		metrics.HeapSysBytes = uint64(val)
	}
	if val, ok := data["heap_inuse_bytes"].(float64); ok {
		metrics.HeapInuseBytes = uint64(val)
	}
	if val, ok := data["container_usage_bytes"].(float64); ok {
		usageBytes := uint64(val)
		metrics.ContainerUsageBytes = &usageBytes
	}
	if val, ok := data["container_limit_bytes"].(float64); ok {
		limitBytes := uint64(val)
		metrics.ContainerLimitBytes = &limitBytes
	}

	return metrics
}
