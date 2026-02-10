package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
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
	taskRepo         storage.TaskRepository
	propletRepo      storage.PropletRepository
	taskPropletRepo  storage.TaskPropletRepository
	metricsRepo      storage.MetricsRepository
	scheduler        scheduler.Scheduler
	baseTopic        string
	pubsub           mqtt.PubSub
	logger           *slog.Logger
	flCoordinatorURL string
	httpClient       *http.Client
}

func NewService(
	repos *storage.Repositories,
	s scheduler.Scheduler, pubsub mqtt.PubSub,
	domainID, channelID string, logger *slog.Logger,
) Service {
	coordinatorURL := os.Getenv("COORDINATOR_URL")
	var httpClient *http.Client
	if coordinatorURL != "" {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
		logger.Info("HTTP FL Coordinator enabled", "url", coordinatorURL)
	} else {
		logger.Warn("COORDINATOR_URL not configured - FL features will not be available")
	}

	svc := &service{
		taskRepo:         repos.Tasks,
		propletRepo:      repos.Proplets,
		taskPropletRepo:  repos.TaskProplets,
		metricsRepo:      repos.Metrics,
		scheduler:        s,
		baseTopic:        fmt.Sprintf(baseTopic, domainID, channelID),
		pubsub:           pubsub,
		logger:           logger,
		flCoordinatorURL: coordinatorURL,
		httpClient:       httpClient,
	}

	return svc
}

func (svc *service) GetProplet(ctx context.Context, propletID string) (proplet.Proplet, error) {
	w, err := svc.propletRepo.Get(ctx, propletID)
	if err != nil {
		return proplet.Proplet{}, err
	}
	w.SetAlive()

	return w, nil
}

func (svc *service) ListProplets(ctx context.Context, offset, limit uint64) (proplet.PropletPage, error) {
	proplets, total, err := svc.propletRepo.List(ctx, offset, limit)
	if err != nil {
		return proplet.PropletPage{}, err
	}
	for i := range proplets {
		proplets[i].SetAlive()
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

func (svc *service) DeleteProplet(ctx context.Context, propletID string) error {
	p, err := svc.propletRepo.Get(ctx, propletID)
	if err != nil {
		return err
	}

	if p.TaskCount > 0 {
		return fmt.Errorf("%w: proplet %s has %d active tasks", pkgerrors.ErrConflict, propletID, p.TaskCount)
	}

	return svc.propletRepo.Delete(ctx, propletID)
}

func (svc *service) CreateTask(ctx context.Context, t task.Task) (task.Task, error) {
	t.ID = uuid.NewString()
	t.CreatedAt = time.Now()

	// Set default kind if not specified
	if t.Kind == "" {
		t.Kind = task.TaskKindStandard
	}

	t, err := svc.taskRepo.Create(ctx, t)
	if err != nil {
		return task.Task{}, err
	}

	return t, nil
}

func (svc *service) GetTask(ctx context.Context, taskID string) (task.Task, error) {
	return svc.taskRepo.Get(ctx, taskID)
}

func (svc *service) ListTasks(ctx context.Context, offset, limit uint64) (task.TaskPage, error) {
	tasks, total, err := svc.taskRepo.List(ctx, offset, limit)
	if err != nil {
		return task.TaskPage{}, err
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

	if err := svc.taskRepo.Update(ctx, dbT); err != nil {
		return task.Task{}, err
	}

	return dbT, nil
}

func (svc *service) DeleteTask(ctx context.Context, taskID string) error {
	return svc.taskRepo.Delete(ctx, taskID)
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
		"encrypted":          t.Encrypted,
		"kbs_resource_path":  t.KBSResourcePath,
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

	if err := svc.pinTaskToProplet(ctx, taskID, p.ID); err != nil {
		return err
	}

	t.PropletID = p.ID

	if err := svc.persistTaskBeforeStart(ctx, &t); err != nil {
		return err
	}

	if err := svc.publishStart(ctx, t, p.ID); err != nil {
		_ = svc.taskPropletRepo.Delete(ctx, taskID)

		return err
	}

	if err := svc.bumpPropletTaskCount(ctx, p, +1); err != nil {
		return err
	}

	if err := svc.markTaskRunning(ctx, &t); err != nil {
		return err
	}

	return nil
}

func (svc *service) StopTask(ctx context.Context, taskID string) error {
	t, err := svc.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	propletID, err := svc.taskPropletRepo.Get(ctx, taskID)
	if err != nil {
		return err
	}
	p, err := svc.GetProplet(ctx, propletID)
	if err != nil {
		return err
	}

	stopPayload := map[string]any{
		"id":         t.ID,
		"proplet_id": propletID,
	}

	topic := svc.baseTopic + "/control/manager/stop"
	if err := svc.pubsub.Publish(ctx, topic, stopPayload); err != nil {
		return err
	}

	if err := svc.taskPropletRepo.Delete(ctx, taskID); err != nil {
		return err
	}

	if err := svc.bumpPropletTaskCount(ctx, p, -1); err != nil {
		return err
	}

	return nil
}

func (svc *service) Subscribe(ctx context.Context) error {
	topic := svc.baseTopic + "/#"
	if err := svc.pubsub.Subscribe(ctx, topic, svc.handle(ctx)); err != nil {
		return err
	}

	flRoundStartTopic := svc.baseTopic + "/fl/rounds/start"
	if err := svc.pubsub.Subscribe(ctx, flRoundStartTopic, svc.handleRoundStart(ctx)); err != nil {
		return err
	}

	return nil
}

func (svc *service) GetTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) (TaskMetricsPage, error) {
	metrics, total, err := svc.metricsRepo.ListTaskMetrics(ctx, taskID, offset, limit)
	if err != nil {
		return TaskMetricsPage{}, err
	}

	return TaskMetricsPage{
		Offset:  offset,
		Limit:   limit,
		Total:   total,
		Metrics: metrics,
	}, nil
}

func (svc *service) GetPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) (PropletMetricsPage, error) {
	metrics, total, err := svc.metricsRepo.ListPropletMetrics(ctx, propletID, offset, limit)
	if err != nil {
		return PropletMetricsPage{}, err
	}

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
	if err := svc.propletRepo.Create(ctx, p); err != nil {
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
	if err := svc.propletRepo.Update(ctx, p); err != nil {
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

	if err := svc.taskRepo.Update(ctx, t); err != nil {
		return err
	}

	return nil
}

//nolint:gocognit // Complex function handles round start orchestration with multiple validation steps
func (svc *service) handleRoundStart(ctx context.Context) func(topic string, msg map[string]any) error {
	return func(topic string, msg map[string]any) error {
		go func() {
			roundCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			roundID, ok := msg["round_id"].(string)
			if !ok || roundID == "" {
				svc.logger.ErrorContext(roundCtx, "missing or invalid round_id")

				return
			}

			if roundCtx.Err() != nil {
				svc.logger.WarnContext(roundCtx, "context cancelled before processing round start", "round_id", roundID)

				return
			}

			modelURI, ok := msg["model_uri"].(string)
			if !ok || modelURI == "" {
				svc.logger.ErrorContext(roundCtx, "missing or invalid model_uri")

				return
			}

			taskWasmImage, ok := msg["task_wasm_image"].(string)
			if !ok || taskWasmImage == "" {
				svc.logger.ErrorContext(roundCtx, "missing or invalid task_wasm_image")

				return
			}

			participantsRaw, ok := msg["participants"].([]any)
			if !ok || len(participantsRaw) == 0 {
				svc.logger.ErrorContext(roundCtx, "missing or invalid participants")

				return
			}

			hyperparams, _ := msg["hyperparams"].(map[string]any)

			participants := make([]string, 0, len(participantsRaw))
			for _, p := range participantsRaw {
				if pid, ok := p.(string); ok && pid != "" {
					participants = append(participants, pid)
				}
			}

			if len(participants) == 0 {
				svc.logger.ErrorContext(roundCtx, "no valid participants")

				return
			}

			for _, propletID := range participants {
				select {
				case <-roundCtx.Done():
					svc.logger.WarnContext(roundCtx, "context cancelled during round processing", "round_id", roundID, "processed", len(participants))

					return
				default:
				}

				p, err := svc.GetProplet(roundCtx, propletID)
				if err != nil {
					svc.logger.WarnContext(roundCtx, "skipping participant: proplet not found", "proplet_id", propletID, "error", err)

					continue
				}
				if !p.Alive {
					svc.logger.WarnContext(roundCtx, "skipping participant: proplet not alive", "proplet_id", propletID)

					continue
				}

				t := task.Task{
					Name:     fmt.Sprintf("fl-round-%s-%s", roundID, propletID),
					Kind:     task.TaskKindStandard,
					State:    task.Pending,
					ImageURL: taskWasmImage,
					Env: map[string]string{
						"ROUND_ID":  roundID,
						"MODEL_URI": modelURI,
					},
					PropletID: propletID,
					CreatedAt: time.Now(),
				}

				if hyperparams != nil {
					hyperparamsJSON, err := json.Marshal(hyperparams)
					if err == nil {
						t.Env["HYPERPARAMS"] = string(hyperparamsJSON)
					}
				}

				created, err := svc.CreateTask(roundCtx, t)
				if err != nil {
					if roundCtx.Err() != nil {
						svc.logger.WarnContext(roundCtx, "context cancelled during task creation", "round_id", roundID, "proplet_id", propletID)

						return
					}
					svc.logger.ErrorContext(roundCtx, "failed to create task for participant", "proplet_id", propletID, "error", err)

					continue
				}

				if err := svc.StartTask(roundCtx, created.ID); err != nil {
					if roundCtx.Err() != nil {
						svc.logger.WarnContext(roundCtx, "context cancelled during task start", "round_id", roundID, "proplet_id", propletID, "task_id", created.ID)

						return
					}
					svc.logger.ErrorContext(roundCtx, "failed to start task for participant", "proplet_id", propletID, "task_id", created.ID, "error", err)

					continue
				}

				svc.logger.InfoContext(roundCtx, "launched task for FL round participant", "round_id", roundID, "proplet_id", propletID, "task_id", created.ID)
			}
		}()

		return nil
	}
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

	if err := svc.metricsRepo.CreateTaskMetrics(ctx, taskMetrics); err != nil {
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

	if err := svc.metricsRepo.CreatePropletMetrics(ctx, propletMetrics); err != nil {
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

func (svc *service) pinTaskToProplet(ctx context.Context, taskID, propletID string) error {
	return svc.taskPropletRepo.Create(ctx, taskID, propletID)
}

func (svc *service) persistTaskBeforeStart(ctx context.Context, t *task.Task) error {
	t.UpdatedAt = time.Now()

	return svc.taskRepo.Update(ctx, *t)
}

func (svc *service) publishStart(ctx context.Context, t task.Task, propletID string) error {
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
		"proplet_id":         propletID,
	}

	topic := svc.baseTopic + "/control/manager/start"

	return svc.pubsub.Publish(ctx, topic, payload)
}

func (svc *service) bumpPropletTaskCount(ctx context.Context, p proplet.Proplet, delta int64) error {
	newCount := max(int64(p.TaskCount)+delta, 0)
	p.TaskCount = uint64(newCount)

	return svc.propletRepo.Update(ctx, p)
}

func (svc *service) markTaskRunning(ctx context.Context, t *task.Task) error {
	t.State = task.Running
	t.StartTime = time.Now()
	t.UpdatedAt = time.Now()

	return svc.taskRepo.Update(ctx, *t)
}
