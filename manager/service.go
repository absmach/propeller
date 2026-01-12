package manager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/0x6flab/namegenerator"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	flpkg "github.com/absmach/propeller/pkg/fl"
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

	modeTrain = task.ModeTrain
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
	aggMu         sync.Mutex
	aggregated    map[string]bool
}

type roundProgressInfo struct {
	Expected     uint64
	Completed    uint64
	Updates      []flpkg.UpdateEnvelope
	Format       string
	TotalSamples uint64
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
		aggregated:    make(map[string]bool),
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

	proplets := make([]proplet.Proplet, 0, len(data))
	for i := range data {
		w, ok := data[i].(proplet.Proplet)
		if !ok {
			return proplet.PropletPage{}, pkgerrors.ErrInvalidData
		}
		w.SetAlive()
		proplets = append(proplets, w)
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

	// Set default kind if not specified
	if t.Kind == "" {
		if t.FL != nil {
			t.Kind = task.TaskKindFederated
		} else {
			t.Kind = task.TaskKindStandard
		}
	}

	// Set default mode for FL tasks
	if t.Kind == task.TaskKindFederated && t.Mode == "" {
		t.Mode = task.ModeInfer
	}

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

	tasks := make([]task.Task, 0, len(data))
	for i := range data {
		t, ok := data[i].(task.Task)
		if !ok {
			return task.TaskPage{}, pkgerrors.ErrInvalidData
		}
		tasks = append(tasks, t)
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

	if err := svc.pinTaskToProplet(ctx, taskID, p.ID); err != nil {
		return err
	}

	t.PropletID = p.ID

	svc.injectFLEnv(ctx, &t)

	if err := svc.persistTaskBeforeStart(ctx, &t); err != nil {
		return err
	}

	if err := svc.publishStart(ctx, t, p.ID); err != nil {
		_ = svc.taskPropletDB.Delete(ctx, taskID)

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

	data, err := svc.taskPropletDB.Get(ctx, taskID)
	if err != nil {
		return err
	}
	propellerID, ok := data.(string)
	if !ok || propellerID == "" {
		return pkgerrors.ErrInvalidData
	}

	p, err := svc.GetProplet(ctx, propellerID)
	if err != nil {
		return err
	}

	stopPayload := map[string]any{
		"id":         t.ID,
		"proplet_id": propellerID,
	}

	topic := svc.baseTopic + "/control/manager/stop"
	if err := svc.pubsub.Publish(ctx, topic, stopPayload); err != nil {
		return err
	}

	if err := svc.taskPropletDB.Delete(ctx, taskID); err != nil {
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

	// Handle FL training tasks
	if t.FL != nil && t.Mode == modeTrain {
		envlp, err := svc.parseAndValidateTrainResults(ctx, t, msg)
		if err != nil {
			svc.logger.ErrorContext(ctx, "failed to parse FL results", "error", err, "task_id", taskID)
			t.Error = err.Error()
			t.State = task.Failed
			t.UpdatedAt = time.Now()
			t.FinishTime = time.Now()
			_ = svc.tasksDB.Update(ctx, t.ID, t)
			return err
		}

		if err := svc.completeTrainTask(ctx, t, envlp, msg); err != nil {
			return err
		}

		// Trigger aggregation check
		if err := svc.tryAggregateAndAdvance(ctx, envlp.JobID, envlp.RoundID); err != nil {
			svc.logger.WarnContext(ctx, "failed to aggregate or advance round", "error", err, "job_id", envlp.JobID, "round_id", envlp.RoundID)
			// Don't fail the task update if aggregation fails
		}

		return nil
	}

	// Handle standard tasks
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

func (svc *service) parseAndValidateTrainResults(ctx context.Context, t task.Task, msg map[string]interface{}) (flpkg.UpdateEnvelope, error) {
	rawRes, exists := msg["results"]
	if !exists {
		return flpkg.UpdateEnvelope{}, errors.New("missing results for train task")
	}

	resBytes, err := json.Marshal(rawRes)
	if err != nil {
		return flpkg.UpdateEnvelope{}, err
	}

	var envlp flpkg.UpdateEnvelope
	if err := json.Unmarshal(resBytes, &envlp); err != nil {
		return flpkg.UpdateEnvelope{}, err
	}

	if t.FL == nil {
		return flpkg.UpdateEnvelope{}, errors.New("train task missing FL spec")
	}
	if envlp.JobID == "" {
		return flpkg.UpdateEnvelope{}, errors.New("invalid results: job_id is empty")
	}
	if envlp.JobID != t.FL.JobID || envlp.RoundID != t.FL.RoundID {
		return flpkg.UpdateEnvelope{}, fmt.Errorf(
			"invalid results: job/round mismatch (got job=%s round=%d, expected job=%s round=%d)",
			envlp.JobID, envlp.RoundID, t.FL.JobID, t.FL.RoundID,
		)
	}

	expectedPID, err := svc.expectedPropletID(ctx, t.ID)
	if err != nil {
		return flpkg.UpdateEnvelope{}, err
	}
	if envlp.PropletID == "" {
		return flpkg.UpdateEnvelope{}, errors.New("invalid results: proplet_id is empty")
	}
	if envlp.PropletID != expectedPID {
		return flpkg.UpdateEnvelope{}, fmt.Errorf(
			"invalid results: proplet_id mismatch (got %s, expected %s)",
			envlp.PropletID, expectedPID,
		)
	}

	return envlp, nil
}

func (svc *service) expectedPropletID(ctx context.Context, taskID string) (string, error) {
	expectedPIDAny, err := svc.taskPropletDB.Get(ctx, taskID)
	if err != nil {
		return "", err
	}

	expectedPID, ok := expectedPIDAny.(string)
	if !ok || expectedPID == "" {
		return "", pkgerrors.ErrInvalidData
	}

	return expectedPID, nil
}

func (svc *service) completeTrainTask(ctx context.Context, t task.Task, envlp flpkg.UpdateEnvelope, msg map[string]interface{}) error {
	t.Results = envlp
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

func (svc *service) tryAggregateAndAdvance(ctx context.Context, jobID string, roundID uint64) error {
	rp, err := svc.roundProgress(ctx, jobID, roundID)
	if err != nil {
		return err
	}
	if rp.Expected == 0 || rp.Completed < rp.Expected {
		return nil
	}

	aggEnv, already, err := svc.aggregateOnce(ctx, jobID, roundID, rp.Updates, rp.Format, rp.TotalSamples)
	if err != nil {
		return err
	}
	if already {
		return nil
	}

	topic := svc.baseTopic + "/control/manager/fl/aggregated"
	payload := map[string]any{
		"job_id":          aggEnv.JobID,
		"round_id":        aggEnv.RoundID,
		"global_version":  aggEnv.GlobalVersion,
		"update_b64":      aggEnv.UpdateB64,
		"format":          aggEnv.Format,
		"metrics":         aggEnv.Metrics,
		"num_samples":     aggEnv.NumSamples,
		"aggregated_from": len(rp.Updates),
	}
	if err := svc.pubsub.Publish(ctx, topic, payload); err != nil {
		return err
	}

	if err := svc.startNextRound(ctx, jobID, roundID, aggEnv); err != nil {
		svc.unmarkAggregated(jobID, roundID)

		return err
	}

	return nil
}

func (svc *service) aggregateOnce(
	ctx context.Context,
	jobID string,
	roundID uint64,
	updates []flpkg.UpdateEnvelope,
	fmtStr string,
	totalSamples uint64,
) (flpkg.UpdateEnvelope, bool, error) {
	aggKey := fmt.Sprintf("%s:%d", jobID, roundID)

	svc.aggMu.Lock()
	already := svc.aggregated[aggKey]
	if !already {
		svc.aggregated[aggKey] = true
	}
	svc.aggMu.Unlock()

	if already {
		return flpkg.UpdateEnvelope{}, true, nil
	}

	aggEnv, err := svc.aggregateRound(jobID, roundID, updates, fmtStr, totalSamples)
	if err != nil {
		svc.unmarkAggregated(jobID, roundID)

		return flpkg.UpdateEnvelope{}, false, err
	}

	storeKey := fmt.Sprintf("fl/%s/%d/aggregate", jobID, roundID)
	if err := svc.tasksDB.Create(ctx, storeKey, aggEnv); err != nil {
		_ = svc.tasksDB.Update(ctx, storeKey, aggEnv)
	}

	return aggEnv, false, nil
}

func (svc *service) unmarkAggregated(jobID string, roundID uint64) {
	aggKey := fmt.Sprintf("%s:%d", jobID, roundID)

	svc.aggMu.Lock()
	delete(svc.aggregated, aggKey)
	svc.aggMu.Unlock()
}

func (svc *service) roundProgress(
	ctx context.Context,
	jobID string,
	roundID uint64,
) (roundProgressInfo, error) {
	all, err := svc.listAllTasks(ctx)
	if err != nil {
		return roundProgressInfo{}, err
	}

	expectedProplets := make(map[string]struct{})
	byProplet := make(map[string]flpkg.UpdateEnvelope)

	fmtChosen := ""
	fmtAny := false

	for i := range all {
		t := all[i]

		if !svc.isRoundTrainTask(t, jobID, roundID) {
			continue
		}

		pid := svc.resolveExpectedPropletID(ctx, t)
		if pid != "" {
			expectedProplets[pid] = struct{}{}
		}

		if !svc.isCompletedWithoutError(t) {
			continue
		}

		env, ok := svc.extractEnvelope(t.Results)
		if !ok || !svc.isMatchingEnvelope(env, jobID, roundID) {
			continue
		}

		byProplet[env.PropletID] = env

		if env.Format != "" && !fmtAny {
			fmtChosen = env.Format
			fmtAny = true
		}
	}

	expected := uint64(len(expectedProplets))
	completed := uint64(len(byProplet))
	updates, totalSamples := svc.flattenEnvelopes(byProplet)

	return roundProgressInfo{
		Expected:     expected,
		Completed:    completed,
		Updates:      updates,
		Format:       fmtChosen,
		TotalSamples: totalSamples,
	}, nil
}

func (svc *service) isRoundTrainTask(t task.Task, jobID string, roundID uint64) bool {
	if t.Mode != modeTrain || t.FL == nil {
		return false
	}

	return t.FL.JobID == jobID && t.FL.RoundID == roundID
}

func (svc *service) resolveExpectedPropletID(ctx context.Context, t task.Task) string {
	if t.PropletID != "" {
		return t.PropletID
	}

	pidAny, err := svc.taskPropletDB.Get(ctx, t.ID)
	if err != nil {
		return ""
	}

	pid, ok := pidAny.(string)
	if !ok {
		return ""
	}

	return pid
}

func (svc *service) isCompletedWithoutError(t task.Task) bool {
	return t.State == task.Completed && t.Error == ""
}

func (svc *service) isMatchingEnvelope(env flpkg.UpdateEnvelope, jobID string, roundID uint64) bool {
	if env.JobID != jobID || env.RoundID != roundID {
		return false
	}

	return env.PropletID != ""
}

func (svc *service) flattenEnvelopes(m map[string]flpkg.UpdateEnvelope) (updates []flpkg.UpdateEnvelope, totalSamples uint64) {
	updates = make([]flpkg.UpdateEnvelope, 0, len(m))
	for _, env := range m {
		updates = append(updates, env)
		totalSamples += env.NumSamples
	}

	return
}

func (svc *service) extractEnvelope(v any) (flpkg.UpdateEnvelope, bool) {
	switch vv := v.(type) {
	case flpkg.UpdateEnvelope:
		return vv, true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return flpkg.UpdateEnvelope{}, false
		}
		var env flpkg.UpdateEnvelope
		if err := json.Unmarshal(b, &env); err != nil {
			return flpkg.UpdateEnvelope{}, false
		}
		if env.JobID == "" {
			return flpkg.UpdateEnvelope{}, false
		}

		return env, true
	}
}

func (svc *service) listAllTasks(ctx context.Context) ([]task.Task, error) {
	var out []task.Task

	var offset uint64
	limit := uint64(defLimit)

	for {
		data, _, err := svc.tasksDB.List(ctx, offset, limit)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			break
		}

		for i := range data {
			t, ok := data[i].(task.Task)
			if !ok {
				return nil, pkgerrors.ErrInvalidData
			}
			out = append(out, t)
		}

		if uint64(len(data)) < limit {
			break
		}
		offset += limit
	}

	return out, nil
}

func (svc *service) aggregateRound(
	jobID string,
	roundID uint64,
	updates []flpkg.UpdateEnvelope,
	fmtStr string,
	totalSamples uint64,
) (flpkg.UpdateEnvelope, error) {
	globalVersion := uuid.NewString()

	if fmtStr == "json-f64" {
		return svc.aggregateJSONF64(jobID, roundID, updates, totalSamples, globalVersion)
	}

	return svc.aggregateConcat(jobID, roundID, updates, totalSamples, globalVersion, fmtStr)
}

func (svc *service) aggregateJSONF64(
	jobID string,
	roundID uint64,
	updates []flpkg.UpdateEnvelope,
	totalSamples uint64,
	globalVersion string,
) (flpkg.UpdateEnvelope, error) {
	if totalSamples == 0 {
		return flpkg.UpdateEnvelope{}, errors.New("cannot aggregate: total_samples is zero")
	}

	var sum []float64
	var dim int
	var haveDim bool

	for i := range updates {
		u := updates[i]

		raw, err := base64.StdEncoding.DecodeString(u.UpdateB64)
		if err != nil {
			return flpkg.UpdateEnvelope{}, fmt.Errorf("invalid update_b64: %w", err)
		}

		var vec []float64
		if err := json.Unmarshal(raw, &vec); err != nil {
			return flpkg.UpdateEnvelope{}, fmt.Errorf("invalid json-f64 payload: %w", err)
		}

		if !haveDim {
			dim = len(vec)
			if dim == 0 {
				return flpkg.UpdateEnvelope{}, errors.New("invalid vector: empty")
			}
			sum = make([]float64, dim)
			haveDim = true
		}
		if len(vec) != dim {
			return flpkg.UpdateEnvelope{}, errors.New("cannot aggregate: mismatched vector dimensions")
		}

		w := float64(u.NumSamples)
		for j := range vec {
			sum[j] += vec[j] * w
		}
	}

	den := float64(totalSamples)
	for i := range sum {
		sum[i] /= den
	}

	avgJSON, err := json.Marshal(sum)
	if err != nil {
		return flpkg.UpdateEnvelope{}, err
	}

	return flpkg.UpdateEnvelope{
		JobID:         jobID,
		RoundID:       roundID,
		GlobalVersion: globalVersion,
		PropletID:     "manager",
		NumSamples:    totalSamples,
		UpdateB64:     base64.StdEncoding.EncodeToString(avgJSON),
		Metrics: map[string]any{
			"num_clients":   len(updates),
			"total_samples": totalSamples,
			"aggregated_at": time.Now().UTC().Format(time.RFC3339),
		},
		Format: "json-f64",
	}, nil
}

func (svc *service) aggregateConcat(
	jobID string,
	roundID uint64,
	updates []flpkg.UpdateEnvelope,
	totalSamples uint64,
	globalVersion string,
	fmtStr string,
) (flpkg.UpdateEnvelope, error) {
	const delim = "\n---PROP-UPDATE---\n"

	var buf []byte
	for i, u := range updates {
		raw, err := base64.StdEncoding.DecodeString(u.UpdateB64)
		if err != nil {
			return flpkg.UpdateEnvelope{}, fmt.Errorf("invalid update_b64: %w", err)
		}
		if i > 0 {
			buf = append(buf, []byte(delim)...)
		}
		buf = append(buf, raw...)
	}

	return flpkg.UpdateEnvelope{
		JobID:         jobID,
		RoundID:       roundID,
		GlobalVersion: globalVersion,
		PropletID:     "manager",
		NumSamples:    totalSamples,
		UpdateB64:     base64.StdEncoding.EncodeToString(buf),
		Metrics: map[string]any{
			"num_clients":   len(updates),
			"total_samples": totalSamples,
			"aggregated_at": time.Now().UTC().Format(time.RFC3339),
		},
		Format: fmtStr,
	}, nil
}

func (svc *service) getAggregatedEnvelope(ctx context.Context, jobID string, roundID uint64) (flpkg.UpdateEnvelope, bool) {
	storeKey := fmt.Sprintf("fl/%s/%d/aggregate", jobID, roundID)
	data, err := svc.tasksDB.Get(ctx, storeKey)
	if err != nil {
		return flpkg.UpdateEnvelope{}, false
	}

	switch v := data.(type) {
	case flpkg.UpdateEnvelope:
		return v, true
	default:
		b, mErr := json.Marshal(data)
		if mErr != nil {
			return flpkg.UpdateEnvelope{}, false
		}
		var env flpkg.UpdateEnvelope
		if uErr := json.Unmarshal(b, &env); uErr != nil {
			return flpkg.UpdateEnvelope{}, false
		}
		if env.JobID == "" {
			return flpkg.UpdateEnvelope{}, false
		}

		return env, true
	}
}

func (svc *service) startNextRound(ctx context.Context, jobID string, currentRound uint64, aggEnv flpkg.UpdateEnvelope) error {
	nextRound := currentRound + 1

	exists, err := svc.roundTasksExist(ctx, jobID, nextRound)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	curTasks, err := svc.tasksForRound(ctx, jobID, currentRound)
	if err != nil {
		return err
	}
	if len(curTasks) == 0 {
		return errors.New("cannot start next round: no tasks found for current round")
	}

	for i := range curTasks {
		cur := curTasks[i]
		if cur.FL == nil || cur.Mode != modeTrain {
			continue
		}

		pinnedPropletID := svc.resolvePinnedPropletID(ctx, cur)
		newEnv := svc.buildNextRoundEnv(cur.Env, jobID, nextRound, aggEnv, cur.FL)
		nextTask := svc.buildNextRoundTask(cur, pinnedPropletID, jobID, nextRound, aggEnv, newEnv)

		created, err := svc.CreateTask(ctx, nextTask)
		if err != nil {
			return err
		}
		if err := svc.StartTask(ctx, created.ID); err != nil {
			return err
		}
	}

	return nil
}

func (svc *service) resolvePinnedPropletID(ctx context.Context, cur task.Task) string {
	var pinnedPropletID string
	if pidAny, gErr := svc.taskPropletDB.Get(ctx, cur.ID); gErr == nil {
		if pid, ok := pidAny.(string); ok && pid != "" {
			pinnedPropletID = pid
		}
	}
	if pinnedPropletID == "" {
		pinnedPropletID = cur.PropletID
	}

	return pinnedPropletID
}

func (svc *service) buildNextRoundEnv(
	curEnv map[string]string,
	jobID string,
	nextRound uint64,
	aggEnv flpkg.UpdateEnvelope,
	flSpec *task.FLSpec,
) map[string]string {
	newEnv := copyStringMap(curEnv)
	if newEnv == nil {
		newEnv = make(map[string]string)
	}

	newEnv["FL_JOB_ID"] = jobID
	newEnv["FL_ROUND_ID"] = strconv.FormatUint(nextRound, 10)
	newEnv["FL_GLOBAL_VERSION"] = aggEnv.GlobalVersion
	newEnv["FL_GLOBAL_UPDATE_B64"] = aggEnv.UpdateB64
	if aggEnv.Format != "" {
		newEnv["FL_GLOBAL_UPDATE_FORMAT"] = aggEnv.Format
	}

	if newEnv["FL_NUM_SAMPLES"] == "" {
		newEnv["FL_NUM_SAMPLES"] = "1"
	}

	updateFormat := ""
	if flSpec != nil {
		updateFormat = flSpec.UpdateFormat
	}
	if updateFormat == "" && aggEnv.Format != "" {
		updateFormat = aggEnv.Format
	}
	if updateFormat != "" {
		newEnv["FL_FORMAT"] = updateFormat
	}

	return newEnv
}

func (svc *service) buildNextRoundTask(
	cur task.Task,
	pinnedPropletID string,
	jobID string,
	nextRound uint64,
	aggEnv flpkg.UpdateEnvelope,
	newEnv map[string]string,
) task.Task {
	updateFormat := ""
	if cur.FL != nil {
		updateFormat = cur.FL.UpdateFormat
	}
	if updateFormat == "" && aggEnv.Format != "" {
		updateFormat = aggEnv.Format
	}

	return task.Task{
		Name:      cur.Name,
		State:     task.Pending,
		ImageURL:  cur.ImageURL,
		File:      cur.File,
		CLIArgs:   append([]string(nil), cur.CLIArgs...),
		Inputs:    append([]uint64(nil), cur.Inputs...),
		Env:       newEnv,
		Daemon:    cur.Daemon,
		PropletID: pinnedPropletID,
		Mode:      task.ModeTrain,
		FL: &task.FLSpec{
			JobID:         jobID,
			RoundID:       nextRound,
			GlobalVersion: aggEnv.GlobalVersion,
			Algorithm:     cur.FL.Algorithm,
			UpdateFormat:  updateFormat,
			Hyperparams:   cur.FL.Hyperparams,
			ModelRef:      cur.FL.ModelRef,
		},
		CreatedAt: time.Now(),
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

// Helper functions for task management
func (svc *service) pinTaskToProplet(ctx context.Context, taskID, propletID string) error {
	return svc.taskPropletDB.Create(ctx, taskID, propletID)
}

func (svc *service) injectFLEnv(ctx context.Context, t *task.Task) {
	if t.FL == nil {
		return
	}
	if t.Env == nil {
		t.Env = make(map[string]string)
	}
	t.Env["FL_JOB_ID"] = t.FL.JobID
	t.Env["FL_ROUND_ID"] = strconv.FormatUint(t.FL.RoundID, 10)
	t.Env["FL_GLOBAL_VERSION"] = t.FL.GlobalVersion
	if t.FL.UpdateFormat != "" {
		t.Env["FL_FORMAT"] = t.FL.UpdateFormat
	}
	if t.FL.ModelRef != "" {
		t.Env["FL_MODEL_REF"] = t.FL.ModelRef
	}
}

func (svc *service) persistTaskBeforeStart(ctx context.Context, t *task.Task) error {
	t.UpdatedAt = time.Now()
	return svc.tasksDB.Update(ctx, t.ID, *t)
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
	if t.FL != nil {
		payload["mode"] = string(t.Mode)
		flPayload := map[string]any{
			"job_id":          t.FL.JobID,
			"round_id":        t.FL.RoundID,
			"global_version":  t.FL.GlobalVersion,
			"min_participants": t.FL.MinParticipants,
			"round_timeout_sec": t.FL.RoundTimeoutSec,
			"algorithm":       t.FL.Algorithm,
			"update_format":   t.FL.UpdateFormat,
			"hyperparams":     t.FL.Hyperparams,
			"model_ref":       t.FL.ModelRef,
		}
		payload["fl"] = flPayload
	}

	topic := svc.baseTopic + "/control/manager/start"
	return svc.pubsub.Publish(ctx, topic, payload)
}

func (svc *service) bumpPropletTaskCount(ctx context.Context, p proplet.Proplet, delta int64) error {
	p.TaskCount = uint64(int64(p.TaskCount) + delta)
	if p.TaskCount < 0 {
		p.TaskCount = 0
	}
	return svc.propletsDB.Update(ctx, p.ID, p)
}

func (svc *service) markTaskRunning(ctx context.Context, t *task.Task) error {
	t.State = task.Running
	t.StartTime = time.Now()
	t.UpdatedAt = time.Now()
	return svc.tasksDB.Update(ctx, t.ID, *t)
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	cpy := make(map[string]string, len(m))
	for k, v := range m {
		cpy[k] = v
	}
	return cpy
}

func (svc *service) tasksForRound(ctx context.Context, jobID string, roundID uint64) ([]task.Task, error) {
	all, err := svc.listAllTasks(ctx)
	if err != nil {
		return nil, err
	}
	var result []task.Task
	for i := range all {
		t := all[i]
		if svc.isRoundTrainTask(t, jobID, roundID) {
			result = append(result, t)
		}
	}
	return result, nil
}

func (svc *service) roundTasksExist(ctx context.Context, jobID string, roundID uint64) (bool, error) {
	all, err := svc.listAllTasks(ctx)
	if err != nil {
		return false, err
	}
	for i := range all {
		if svc.isRoundTrainTask(all[i], jobID, roundID) {
			return true, nil
		}
	}
	return false, nil
}
