package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/0x6flab/namegenerator"
	"github.com/absmach/propeller/pkg/cron"
	"github.com/absmach/propeller/pkg/dag"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/task"
	"github.com/google/uuid"
)

const (
	defOffset                 = 0
	defLimit                  = 100
	aliveHistoryLimit         = 10
	defaultPriority           = 50
	defaultTimezone           = "UTC"
	ExecutionModeParallel     = "parallel"
	ExecutionModeSequential   = "sequential"
	ExecutionModeConfigurable = "configurable"
	EnvJobExecutionMode       = "JOB_EXECUTION_MODE"
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
	cronScheduler    CronScheduler
	baseTopic        string
	pubsub           mqtt.PubSub
	logger           *slog.Logger
	flCoordinatorURL string
	httpClient       *http.Client
	coordinator      *WorkflowCoordinator
}

func NewService(
	repos *storage.Repositories,
	s scheduler.Scheduler, pubsub mqtt.PubSub,
	domainID, channelID string, logger *slog.Logger,
) (Service, CronScheduler) {
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
	svc.coordinator = NewWorkflowCoordinator(repos.Tasks, svc, logger)

	cronSched := NewCronScheduler(repos.Tasks, svc, logger)
	svc.cronScheduler = cronSched

	return svc, cronSched
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
	if len(t.DependsOn) > 0 && t.WorkflowID == "" {
		return task.Task{}, errors.New("workflow_id is required when depends_on is specified")
	}

	if len(t.DependsOn) > 0 && t.WorkflowID != "" {
		workflowTasks, err := svc.getWorkflowTasks(ctx, t.WorkflowID)
		if err != nil {
			return task.Task{}, fmt.Errorf("failed to get workflow tasks: %w", err)
		}

		allTasks := make([]task.Task, len(workflowTasks), len(workflowTasks)+1)
		copy(allTasks, workflowTasks)
		allTasks = append(allTasks, t)

		if err := dag.ValidateDependenciesExist(allTasks); err != nil {
			return task.Task{}, fmt.Errorf("dependency validation failed: %w", err)
		}

		if err := dag.ValidateDAG(allTasks); err != nil {
			return task.Task{}, fmt.Errorf("DAG validation failed: %w", err)
		}
	}

	t.ID = uuid.NewString()
	t.CreatedAt = time.Now()

	// Set default kind if not specified
	if t.Kind == "" {
		t.Kind = task.TaskKindStandard
	}

	if t.Priority == 0 {
		t.Priority = defaultPriority
	}

	if t.Schedule != "" {
		schedule, err := cron.ParseCronExpression(t.Schedule)
		if err != nil {
			return task.Task{}, fmt.Errorf("invalid cron expression: %w", err)
		}

		timezone := t.Timezone
		if timezone == "" {
			timezone = defaultTimezone
		}

		t.NextRun = cron.CalculateNextRun(schedule, time.Now(), timezone)
	}

	t, err := svc.taskRepo.Create(ctx, t)
	if err != nil {
		return task.Task{}, err
	}

	if t.Schedule != "" && svc.cronScheduler != nil {
		if err := svc.cronScheduler.ScheduleTask(ctx, t.ID); err != nil {
			svc.logger.WarnContext(ctx, "failed to schedule task in cron scheduler", "error", err, "task_id", t.ID)
		}
	}

	return t, nil
}

func (svc *service) CreateWorkflow(ctx context.Context, tasks []task.Task) ([]task.Task, error) {
	if len(tasks) == 0 {
		return nil, errors.New("workflow must contain at least one task")
	}

	workflowID := uuid.NewString()
	for i := range tasks {
		if tasks[i].WorkflowID != "" {
			workflowID = tasks[i].WorkflowID

			break
		}
	}

	for i := range tasks {
		tasks[i].WorkflowID = workflowID
		if tasks[i].ID == "" {
			tasks[i].ID = uuid.NewString()
		}
		tasks[i].CreatedAt = time.Now()
	}

	if err := dag.ValidateDependenciesExist(tasks); err != nil {
		return nil, fmt.Errorf("dependency validation failed: %w", err)
	}

	if err := dag.ValidateDAG(tasks); err != nil {
		return nil, fmt.Errorf("DAG validation failed: %w", err)
	}

	for i := range tasks {
		if tasks[i].RunIf != "" && tasks[i].RunIf != task.RunIfSuccess && tasks[i].RunIf != task.RunIfFailure {
			return nil, fmt.Errorf("invalid run_if value for task %s: must be 'success' or 'failure'", tasks[i].ID)
		}
	}

	createdTasks := make([]task.Task, 0, len(tasks))
	for i := range tasks {
		t := tasks[i]
		created, err := svc.taskRepo.Create(ctx, t)
		if err != nil {
			for j := range createdTasks {
				created := &createdTasks[j]
				_ = svc.taskRepo.Delete(ctx, created.ID)
			}

			return nil, fmt.Errorf("failed to create task %s: %w", t.ID, err)
		}
		createdTasks = append(createdTasks, created)
	}

	return createdTasks, nil
}

func (svc *service) CreateJob(ctx context.Context, name string, tasks []task.Task, executionMode string) (string, []task.Task, error) {
	if len(tasks) == 0 {
		return "", nil, errors.New("job must contain at least one task")
	}

	jobID := uuid.NewString()
	for i := range tasks {
		if tasks[i].JobID != "" {
			jobID = tasks[i].JobID

			break
		}
	}

	for i := range tasks {
		tasks[i].JobID = jobID
		if tasks[i].ID == "" {
			tasks[i].ID = uuid.NewString()
		}
		tasks[i].CreatedAt = time.Now()

		if executionMode != "" {
			if tasks[i].Env == nil {
				tasks[i].Env = make(map[string]string)
			}
			tasks[i].Env["_job_execution_mode"] = executionMode
		}

		if name != "" {
			if tasks[i].Env == nil {
				tasks[i].Env = make(map[string]string)
			}
			tasks[i].Env["_job_name"] = name
		}
	}

	if err := dag.ValidateDependenciesExist(tasks); err != nil {
		return "", nil, fmt.Errorf("dependency validation failed: %w", err)
	}

	if err := dag.ValidateDAG(tasks); err != nil {
		return "", nil, fmt.Errorf("DAG validation failed: %w", err)
	}

	createdTasks := make([]task.Task, 0, len(tasks))
	for i := range tasks {
		t := tasks[i]
		created, err := svc.taskRepo.Create(ctx, t)
		if err != nil {
			for j := range createdTasks {
				created := createdTasks[j]
				_ = svc.taskRepo.Delete(ctx, created.ID)
			}

			return "", nil, fmt.Errorf("failed to create task %s: %w", t.ID, err)
		}
		createdTasks = append(createdTasks, created)
	}

	return jobID, createdTasks, nil
}

func (svc *service) GetJob(ctx context.Context, jobID string) ([]task.Task, error) {
	return svc.getJobTasks(ctx, jobID)
}

func (svc *service) ListJobs(ctx context.Context, offset, limit uint64) (JobPage, error) {
	allTasks, _, err := svc.taskRepo.List(ctx, 0, 10000)
	if err != nil {
		return JobPage{}, err
	}

	jobMap := make(map[string][]task.Task)
	for i := range allTasks {
		if allTasks[i].JobID != "" {
			jobMap[allTasks[i].JobID] = append(jobMap[allTasks[i].JobID], allTasks[i])
		}
	}

	jobs := make([]JobSummary, 0, len(jobMap))
	for jobID, tasks := range jobMap {
		summary := computeJobSummary(jobID, tasks)
		jobs = append(jobs, summary)
	}
	total := uint64(len(jobs))
	if offset >= total {
		return JobPage{
			Offset: offset,
			Limit:  limit,
			Total:  total,
			Jobs:   []JobSummary{},
		}, nil
	}

	end := min(offset+limit, total)
	paginatedJobs := jobs[offset:end]

	return JobPage{
		Offset: offset,
		Limit:  limit,
		Total:  total,
		Jobs:   paginatedJobs,
	}, nil
}

func computeJobSummary(jobID string, tasks []task.Task) JobSummary {
	if len(tasks) == 0 {
		return JobSummary{
			JobID: jobID,
			State: task.Pending,
			Tasks: []task.Task{},
		}
	}

	name := ""
	if tasks[0].Env != nil {
		if n, ok := tasks[0].Env["_job_name"]; ok {
			name = n
		}
	}

	state := ComputeJobState(tasks)

	var startTime, finishTime, createdAt time.Time
	hasStartTime := false
	hasFinishTime := false
	hasCreatedAt := false

	for i := range tasks {
		t := &tasks[i]
		if !t.CreatedAt.IsZero() {
			if !hasCreatedAt || t.CreatedAt.Before(createdAt) {
				createdAt = t.CreatedAt
				hasCreatedAt = true
			}
		}
		if !t.StartTime.IsZero() {
			if !hasStartTime || t.StartTime.Before(startTime) {
				startTime = t.StartTime
				hasStartTime = true
			}
		}
		if !t.FinishTime.IsZero() {
			if !hasFinishTime || t.FinishTime.After(finishTime) {
				finishTime = t.FinishTime
				hasFinishTime = true
			}
		}
	}

	return JobSummary{
		JobID:      jobID,
		Name:       name,
		State:      state,
		Tasks:      tasks,
		StartTime:  startTime,
		FinishTime: finishTime,
		CreatedAt:  createdAt,
	}
}

func ComputeJobState(tasks []task.Task) task.State {
	if len(tasks) == 0 {
		return task.Pending
	}

	hasFailed := false
	hasRunning := false
	hasScheduled := false
	allCompleted := true
	allSkipped := true

	for i := range tasks {
		t := &tasks[i]
		switch t.State {
		case task.Failed:
			hasFailed = true
			allCompleted = false
			allSkipped = false
		case task.Running:
			hasRunning = true
			allCompleted = false
			allSkipped = false
		case task.Scheduled:
			hasScheduled = true
			allCompleted = false
			allSkipped = false
		case task.Completed:
			allSkipped = false
		case task.Skipped:
		case task.Pending:
			allCompleted = false
			allSkipped = false
		}
	}

	if hasFailed {
		return task.Failed
	}
	if hasRunning || hasScheduled {
		return task.Running
	}
	if allCompleted || allSkipped {
		return task.Completed
	}

	return task.Pending
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
	if t.Priority != 0 {
		dbT.Priority = t.Priority
	}

	scheduleChanged := false
	if t.Schedule != "" && t.Schedule != dbT.Schedule {
		schedule, err := cron.ParseCronExpression(t.Schedule)
		if err != nil {
			return task.Task{}, fmt.Errorf("invalid cron expression: %w", err)
		}

		timezone := t.Timezone
		if timezone == "" {
			timezone = defaultTimezone
		}

		dbT.Schedule = t.Schedule
		dbT.IsRecurring = t.IsRecurring
		dbT.Timezone = timezone
		dbT.NextRun = cron.CalculateNextRun(schedule, time.Now(), timezone)
		scheduleChanged = true
	} else if t.Schedule == "" && dbT.Schedule != "" {
		dbT.Schedule = ""
		dbT.NextRun = time.Time{}
		dbT.IsRecurring = false
		scheduleChanged = true
	}

	if err := svc.taskRepo.Update(ctx, dbT); err != nil {
		return task.Task{}, err
	}

	if !scheduleChanged || svc.cronScheduler == nil {
		return dbT, nil
	}

	if dbT.Schedule != "" {
		if err := svc.cronScheduler.ScheduleTask(ctx, dbT.ID); err != nil {
			svc.logger.WarnContext(ctx, "failed to reschedule task in cron scheduler", "error", err, "task_id", dbT.ID)
		}

		return dbT, nil
	}

	if err := svc.cronScheduler.UnscheduleTask(ctx, dbT.ID); err != nil {
		svc.logger.WarnContext(ctx, "failed to unschedule task in cron scheduler", "error", err, "task_id", dbT.ID)
	}

	return dbT, nil
}

func (svc *service) DeleteTask(ctx context.Context, taskID string) error {
	if svc.cronScheduler != nil {
		if err := svc.cronScheduler.UnscheduleTask(ctx, taskID); err != nil {
			svc.logger.WarnContext(ctx, "failed to unschedule task from cron scheduler", "error", err, "task_id", taskID)
		}
	}

	return svc.taskRepo.Delete(ctx, taskID)
}

func (svc *service) StartTask(ctx context.Context, taskID string) error {
	t, err := svc.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	if len(t.DependsOn) == 0 {
		return svc.startTaskWithoutDeps(ctx, taskID, t)
	}

	if err := svc.checkTaskDependencies(ctx, taskID, t); err != nil {
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

func (svc *service) StartJob(ctx context.Context, jobID string) error {
	tasks, err := svc.getJobTasks(ctx, jobID)
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		return fmt.Errorf("job %s has no tasks", jobID)
	}

	executionMode := strings.TrimSpace(os.Getenv(EnvJobExecutionMode))
	if len(tasks) > 0 && tasks[0].Env != nil {
		if mode, ok := tasks[0].Env["_job_execution_mode"]; ok {
			executionMode = strings.TrimSpace(mode)
		}
	}
	if executionMode == "" {
		return fmt.Errorf("%s must be set", EnvJobExecutionMode)
	}

	if err := dag.ValidateDependenciesExist(tasks); err != nil {
		return fmt.Errorf("dependency validation failed: %w", err)
	}

	if err := dag.ValidateDAG(tasks); err != nil {
		return fmt.Errorf("DAG validation failed: %w", err)
	}

	switch executionMode {
	case ExecutionModeParallel:
		return svc.startJobParallel(ctx, tasks)
	case ExecutionModeSequential:
		return svc.startJobSequential(ctx, tasks)
	case ExecutionModeConfigurable:
		return svc.startJobConfigurable(ctx, tasks)
	default:
		return fmt.Errorf("invalid %s: %q (allowed: %q, %q, %q)", EnvJobExecutionMode, executionMode, ExecutionModeParallel, ExecutionModeSequential, ExecutionModeConfigurable)
	}
}

func (svc *service) StopJob(ctx context.Context, jobID string) error {
	tasks, err := svc.getJobTasks(ctx, jobID)
	if err != nil {
		return err
	}

	svc.stopJobTasks(ctx, tasks)

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

func (svc *service) GetTaskResults(ctx context.Context, taskID string) (any, error) {
	t, err := svc.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	return t.Results, nil
}

func (svc *service) GetParentResults(ctx context.Context, taskID string) (map[string]any, error) {
	t, err := svc.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	parentResults := make(map[string]any)
	for _, depID := range t.DependsOn {
		dep, err := svc.GetTask(ctx, depID)
		if err != nil {
			svc.logger.WarnContext(ctx, "failed to get parent task", "parent_id", depID, "task_id", taskID, "error", err)

			continue
		}
		if dep.State == task.Completed && dep.Results != nil {
			parentResults[depID] = dep.Results
		}
	}

	return parentResults, nil
}

func (svc *service) StartCronScheduler(ctx context.Context) error {
	if svc.cronScheduler == nil {
		return nil
	}

	return svc.cronScheduler.Start(ctx)
}

func (svc *service) checkTaskDependencies(ctx context.Context, taskID string, t task.Task) error {
	allDepsCompleted := true
	for _, depID := range t.DependsOn {
		dep, err := svc.GetTask(ctx, depID)
		if err != nil {
			return fmt.Errorf("failed to get dependency task %s: %w", depID, err)
		}
		if dep.State != task.Completed && dep.State != task.Failed && dep.State != task.Skipped {
			allDepsCompleted = false

			break
		}
	}

	if !allDepsCompleted {
		if t.WorkflowID != "" {
			return svc.coordinator.CheckAndStartReadyTasks(ctx, t.WorkflowID)
		}

		return fmt.Errorf("task %s has unmet dependencies", taskID)
	}

	return nil
}

func (svc *service) startTaskWithoutDeps(ctx context.Context, taskID string, t task.Task) error {
	parentResults, err := svc.GetParentResults(ctx, taskID)
	if err != nil {
		svc.logger.WarnContext(ctx, "failed to get parent results", "task_id", taskID, "error", err)
		parentResults = make(map[string]any)
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
		"parent_results":     parentResults,
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

func (svc *service) startJobParallel(ctx context.Context, tasks []task.Task) error {
	for i := range tasks {
		t := &tasks[i]
		if len(t.DependsOn) == 0 {
			if err := svc.StartTask(ctx, t.ID); err != nil {
				svc.stopJobTasks(ctx, tasks)

				return fmt.Errorf("failed to start task %s: %w", t.ID, err)
			}
		}
	}

	return nil
}

func (svc *service) startJobSequential(ctx context.Context, tasks []task.Task) error {
	taskMap := make(map[string]task.Task)
	for i := range tasks {
		t := &tasks[i]
		taskMap[t.ID] = *t
	}

	readyTasks := make([]task.Task, 0)
	for i := range tasks {
		t := &tasks[i]
		if len(t.DependsOn) == 0 {
			readyTasks = append(readyTasks, *t)
		}
	}

	for i := range readyTasks {
		t := &readyTasks[i]
		if err := svc.StartTask(ctx, t.ID); err != nil {
			svc.stopJobTasks(ctx, tasks)

			return fmt.Errorf("failed to start task %s: %w", t.ID, err)
		}
	}

	return nil
}

func (svc *service) startJobConfigurable(ctx context.Context, tasks []task.Task) error {
	readyTasks := make([]task.Task, 0)
	for i := range tasks {
		t := &tasks[i]
		if len(t.DependsOn) == 0 {
			readyTasks = append(readyTasks, *t)
		}
	}

	for i := range readyTasks {
		t := &readyTasks[i]
		if err := svc.StartTask(ctx, t.ID); err != nil {
			svc.stopJobTasks(ctx, tasks)

			return fmt.Errorf("failed to start task %s: %w", t.ID, err)
		}
	}

	return nil
}

func (svc *service) stopJobTasks(ctx context.Context, tasks []task.Task) {
	for i := range tasks {
		t := &tasks[i]
		if t.State == task.Running || t.State == task.Scheduled {
			_ = svc.StopTask(ctx, t.ID)
		}
	}
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
		t.State = task.Failed
	}

	if err := svc.taskRepo.Update(ctx, t); err != nil {
		return err
	}

	if t.JobID == "" {
		if err := svc.coordinator.OnTaskCompletion(ctx, taskID); err != nil {
			svc.logger.ErrorContext(ctx, "failed to trigger workflow coordinator", "task_id", taskID, "error", err)
		}

		return nil
	}

	jobTasks, err := svc.getJobTasks(ctx, t.JobID)
	if err != nil {
		svc.logger.WarnContext(ctx, "failed to get job tasks", "job_id", t.JobID, "error", err)
		if err := svc.coordinator.OnTaskCompletion(ctx, taskID); err != nil {
			svc.logger.ErrorContext(ctx, "failed to trigger workflow coordinator", "task_id", taskID, "error", err)
		}

		return nil
	}

	if t.State == task.Failed {
		svc.logger.InfoContext(ctx, "task failed, stopping remaining job tasks", "job_id", t.JobID, "task_id", taskID)
		svc.stopJobTasks(ctx, jobTasks)
		if err := svc.coordinator.OnTaskCompletion(ctx, taskID); err != nil {
			svc.logger.ErrorContext(ctx, "failed to trigger workflow coordinator", "task_id", taskID, "error", err)
		}

		return nil
	}

	allCompleted := true
	hasFailed := false
	for i := range jobTasks {
		jobTask := &jobTasks[i]
		if jobTask.State != task.Completed && jobTask.State != task.Skipped {
			allCompleted = false
		}
		if jobTask.State == task.Failed {
			hasFailed = true
		}
	}

	if allCompleted {
		svc.logger.InfoContext(ctx, "job completed", "job_id", t.JobID)
	}

	if !allCompleted && !hasFailed {
		svc.startJobDependentTasks(ctx, jobTasks, taskID)
	}

	if err := svc.coordinator.OnTaskCompletion(ctx, taskID); err != nil {
		svc.logger.ErrorContext(ctx, "failed to trigger workflow coordinator", "task_id", taskID, "error", err)
	}

	return nil
}

func (svc *service) startJobDependentTasks(ctx context.Context, jobTasks []task.Task, completedTaskID string) {
	for i := range jobTasks {
		t := &jobTasks[i]
		if t.State != task.Pending {
			continue
		}

		if !svc.hasDependencyOnTask(t, completedTaskID) {
			continue
		}

		if svc.allDependenciesComplete(ctx, t) {
			if err := svc.StartTask(ctx, t.ID); err != nil {
				svc.logger.WarnContext(ctx, "failed to start dependent task", "task_id", t.ID, "error", err)
			}
		}
	}
}

func (svc *service) hasDependencyOnTask(t *task.Task, taskID string) bool {
	return slices.Contains(t.DependsOn, taskID)
}

func (svc *service) allDependenciesComplete(ctx context.Context, t *task.Task) bool {
	for _, checkDepID := range t.DependsOn {
		dep, err := svc.GetTask(ctx, checkDepID)
		if err != nil {
			return false
		}
		if dep.State != task.Completed && dep.State != task.Skipped && dep.State != task.Failed {
			return false
		}
	}

	return true
}

func (svc *service) handleRoundStart(ctx context.Context) func(topic string, msg map[string]any) error {
	return func(topic string, msg map[string]any) error {
		go svc.processRoundStart(ctx, msg)

		return nil
	}
}

func (svc *service) processRoundStart(ctx context.Context, msg map[string]any) {
	roundCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	roundConfig, err := svc.parseRoundStartMessage(roundCtx, msg)
	if err != nil {
		return
	}

	participants := svc.extractParticipants(roundCtx, msg)
	if len(participants) == 0 {
		return
	}

	svc.launchTasksForParticipants(roundCtx, roundConfig, participants)
}

type roundConfig struct {
	roundID       string
	modelURI      string
	taskWasmImage string
	hyperparams   map[string]any
}

func (svc *service) parseRoundStartMessage(roundCtx context.Context, msg map[string]any) (roundConfig, error) {
	roundID, ok := msg["round_id"].(string)
	if !ok || roundID == "" {
		svc.logger.ErrorContext(roundCtx, "missing or invalid round_id")

		return roundConfig{}, errors.New("invalid round_id")
	}

	if roundCtx.Err() != nil {
		svc.logger.WarnContext(roundCtx, "context cancelled before processing round start", "round_id", roundID)

		return roundConfig{}, roundCtx.Err()
	}

	modelURI, ok := msg["model_uri"].(string)
	if !ok || modelURI == "" {
		svc.logger.ErrorContext(roundCtx, "missing or invalid model_uri")

		return roundConfig{}, errors.New("invalid model_uri")
	}

	taskWasmImage, ok := msg["task_wasm_image"].(string)
	if !ok || taskWasmImage == "" {
		svc.logger.ErrorContext(roundCtx, "missing or invalid task_wasm_image")

		return roundConfig{}, errors.New("invalid task_wasm_image")
	}

	participantsRaw, ok := msg["participants"].([]any)
	if !ok || len(participantsRaw) == 0 {
		svc.logger.ErrorContext(roundCtx, "missing or invalid participants")

		return roundConfig{}, errors.New("invalid participants")
	}

	hyperparams, _ := msg["hyperparams"].(map[string]any)

	return roundConfig{
		roundID:       roundID,
		modelURI:      modelURI,
		taskWasmImage: taskWasmImage,
		hyperparams:   hyperparams,
	}, nil
}

func (svc *service) extractParticipants(roundCtx context.Context, msg map[string]any) []string {
	participantsRaw, ok := msg["participants"].([]any)
	if !ok || len(participantsRaw) == 0 {
		return nil
	}

	participants := make([]string, 0, len(participantsRaw))
	for _, p := range participantsRaw {
		if pid, ok := p.(string); ok && pid != "" {
			participants = append(participants, pid)
		}
	}

	if len(participants) == 0 {
		svc.logger.ErrorContext(roundCtx, "no valid participants")
	}

	return participants
}

func (svc *service) launchTasksForParticipants(roundCtx context.Context, config roundConfig, participants []string) {
	for _, propletID := range participants {
		if roundCtx.Err() != nil {
			svc.logger.WarnContext(roundCtx, "context cancelled during round processing", "round_id", config.roundID, "processed", len(participants))

			return
		}

		if !svc.isPropletAvailable(roundCtx, propletID) {
			continue
		}

		svc.launchTaskForParticipant(roundCtx, config, propletID)
	}
}

func (svc *service) isPropletAvailable(roundCtx context.Context, propletID string) bool {
	p, err := svc.GetProplet(roundCtx, propletID)
	if err != nil {
		svc.logger.WarnContext(roundCtx, "skipping participant: proplet not found", "proplet_id", propletID, "error", err)

		return false
	}

	if !p.Alive {
		svc.logger.WarnContext(roundCtx, "skipping participant: proplet not alive", "proplet_id", propletID)

		return false
	}

	return true
}

func (svc *service) launchTaskForParticipant(roundCtx context.Context, config roundConfig, propletID string) {
	t := svc.createRoundTask(config, propletID)

	created, err := svc.CreateTask(roundCtx, t)
	if err != nil {
		if roundCtx.Err() != nil {
			svc.logger.WarnContext(roundCtx, "context cancelled during task creation", "round_id", config.roundID, "proplet_id", propletID)

			return
		}
		svc.logger.ErrorContext(roundCtx, "failed to create task for participant", "proplet_id", propletID, "error", err)

		return
	}

	if err := svc.StartTask(roundCtx, created.ID); err != nil {
		if roundCtx.Err() != nil {
			svc.logger.WarnContext(roundCtx, "context cancelled during task start", "round_id", config.roundID, "proplet_id", propletID, "task_id", created.ID)

			return
		}
		svc.logger.ErrorContext(roundCtx, "failed to start task for participant", "proplet_id", propletID, "task_id", created.ID, "error", err)

		return
	}

	svc.logger.InfoContext(roundCtx, "launched task for FL round participant", "round_id", config.roundID, "proplet_id", propletID, "task_id", created.ID)
}

func (svc *service) createRoundTask(config roundConfig, propletID string) task.Task {
	t := task.Task{
		Name:      fmt.Sprintf("fl-round-%s-%s", config.roundID, propletID),
		Kind:      task.TaskKindStandard,
		State:     task.Pending,
		ImageURL:  config.taskWasmImage,
		PropletID: propletID,
		CreatedAt: time.Now(),
		Env: map[string]string{
			"ROUND_ID":  config.roundID,
			"MODEL_URI": config.modelURI,
		},
	}

	if config.hyperparams != nil {
		hyperparamsJSON, err := json.Marshal(config.hyperparams)
		if err == nil {
			t.Env["HYPERPARAMS"] = string(hyperparamsJSON)
		}
	}

	return t
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

func (svc *service) getWorkflowTasks(ctx context.Context, workflowID string) ([]task.Task, error) {
	allTasks, _, err := svc.taskRepo.List(ctx, 0, 10000)
	if err != nil {
		return nil, err
	}

	tasks := make([]task.Task, 0)
	for i := range allTasks {
		if allTasks[i].WorkflowID == workflowID {
			tasks = append(tasks, allTasks[i])
		}
	}

	return tasks, nil
}

func (svc *service) getJobTasks(ctx context.Context, jobID string) ([]task.Task, error) {
	allTasks, _, err := svc.taskRepo.List(ctx, 0, 10000)
	if err != nil {
		return nil, err
	}

	tasks := make([]task.Task, 0)
	for i := range allTasks {
		if allTasks[i].JobID == jobID {
			tasks = append(tasks, allTasks[i])
		}
	}

	return tasks, nil
}
