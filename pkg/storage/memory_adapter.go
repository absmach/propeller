package storage

import (
	"context"
	"fmt"
	"sync"
	"time"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/job"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/task"
)

const memoryScanPageSize uint64 = 100

type memoryTaskRepo struct {
	storage Storage
	mu      sync.RWMutex
	// metaIdx[key][value] → set of task IDs
	metaIdx map[string]map[string]map[string]struct{}
}

func newMemoryTaskRepository(s Storage) TaskRepository {
	return &memoryTaskRepo{
		storage: s,
		metaIdx: make(map[string]map[string]map[string]struct{}),
	}
}

func (r *memoryTaskRepo) Create(ctx context.Context, t task.Task) (task.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.storage.Create(ctx, t.ID, t); err != nil {
		return task.Task{}, err
	}
	r.indexTask(t)

	return t, nil
}

func (r *memoryTaskRepo) Get(ctx context.Context, id string) (task.Task, error) {
	data, err := r.storage.Get(ctx, id)
	if err != nil {
		return task.Task{}, err
	}
	t, ok := data.(task.Task)
	if !ok {
		return task.Task{}, pkgerrors.ErrInvalidData
	}

	return t, nil
}

func (r *memoryTaskRepo) Update(ctx context.Context, t task.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, err := r.storage.Get(ctx, t.ID); err == nil {
		if oldTask, ok := old.(task.Task); ok {
			r.deindexTask(oldTask)
		}
	}
	if err := r.storage.Update(ctx, t.ID, t); err != nil {
		return err
	}
	r.indexTask(t)

	return nil
}

func (r *memoryTaskRepo) List(ctx context.Context, offset, limit uint64) ([]task.Task, uint64, error) {
	data, total, err := r.storage.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	tasks := make([]task.Task, len(data))
	for i, d := range data {
		t, ok := d.(task.Task)
		if !ok {
			return nil, 0, pkgerrors.ErrInvalidData
		}
		tasks[i] = t
	}

	return tasks, total, nil
}

func (r *memoryTaskRepo) ListByMetadataFilter(ctx context.Context, filter map[string]string, offset, limit uint64) ([]task.Task, uint64, error) {
	if len(filter) == 0 {
		return r.List(ctx, offset, limit)
	}

	r.mu.RLock()
	var matchIDs map[string]struct{}
	for k, v := range filter {
		vals, ok := r.metaIdx[k]
		if !ok {
			r.mu.RUnlock()

			return []task.Task{}, 0, nil
		}
		taskIDs, ok := vals[v]
		if !ok {
			r.mu.RUnlock()

			return []task.Task{}, 0, nil
		}
		if matchIDs == nil {
			matchIDs = make(map[string]struct{}, len(taskIDs))
			for id := range taskIDs {
				matchIDs[id] = struct{}{}
			}
		} else {
			for id := range matchIDs {
				if _, ok := taskIDs[id]; !ok {
					delete(matchIDs, id)
				}
			}
		}
		if len(matchIDs) == 0 {
			r.mu.RUnlock()

			return []task.Task{}, 0, nil
		}
	}
	r.mu.RUnlock()

	tasks := make([]task.Task, 0, len(matchIDs))
	for id := range matchIDs {
		data, err := r.storage.Get(ctx, id)
		if err != nil {
			continue
		}
		t, ok := data.(task.Task)
		if !ok {
			continue
		}
		tasks = append(tasks, t)
	}

	total := uint64(len(tasks))
	if offset >= total {
		return []task.Task{}, total, nil
	}
	end := min(offset+limit, total)

	return tasks[offset:end], total, nil
}

func (r *memoryTaskRepo) ListByWorkflowID(ctx context.Context, workflowID string) ([]task.Task, error) {
	data, _, err := r.storage.List(ctx, 0, maxMemoryFetch)
	if err != nil {
		return nil, err
	}

	tasks := make([]task.Task, 0)
	for _, d := range data {
		t, ok := d.(task.Task)
		if !ok {
			continue
		}
		if t.WorkflowID == workflowID {
			tasks = append(tasks, t)
		}
	}

	return tasks, nil
}

func (r *memoryTaskRepo) ListByJobID(ctx context.Context, jobID string) ([]task.Task, error) {
	data, _, err := r.storage.List(ctx, 0, maxMemoryFetch)
	if err != nil {
		return nil, err
	}

	tasks := make([]task.Task, 0)
	for _, d := range data {
		t, ok := d.(task.Task)
		if !ok {
			continue
		}
		if t.JobID == jobID {
			tasks = append(tasks, t)
		}
	}

	return tasks, nil
}

func (r *memoryTaskRepo) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, err := r.storage.Get(ctx, id); err == nil {
		if oldTask, ok := old.(task.Task); ok {
			r.deindexTask(oldTask)
		}
	}

	return r.storage.Delete(ctx, id)
}

func (r *memoryTaskRepo) indexTask(t task.Task) {
	for k, v := range t.Metadata {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if r.metaIdx[k] == nil {
			r.metaIdx[k] = make(map[string]map[string]struct{})
		}
		if r.metaIdx[k][s] == nil {
			r.metaIdx[k][s] = make(map[string]struct{})
		}
		r.metaIdx[k][s][t.ID] = struct{}{}
	}
}

func (r *memoryTaskRepo) deindexTask(t task.Task) {
	for k, v := range t.Metadata {
		s, ok := v.(string)
		if !ok {
			continue
		}
		ids, ok := r.metaIdx[k][s]
		if !ok {
			continue
		}
		delete(ids, t.ID)
		if len(ids) == 0 {
			delete(r.metaIdx[k], s)
		}
		if len(r.metaIdx[k]) == 0 {
			delete(r.metaIdx, k)
		}
	}
}

type memoryPropletRepo struct {
	storage Storage
}

func newMemoryPropletRepository(s Storage) PropletRepository {
	return &memoryPropletRepo{storage: s}
}

func (r *memoryPropletRepo) Create(ctx context.Context, p proplet.Proplet) error {
	return r.storage.Create(ctx, p.ID, p)
}

func (r *memoryPropletRepo) Get(ctx context.Context, id string) (proplet.Proplet, error) {
	data, err := r.storage.Get(ctx, id)
	if err != nil {
		return proplet.Proplet{}, err
	}
	p, ok := data.(proplet.Proplet)
	if !ok {
		return proplet.Proplet{}, pkgerrors.ErrInvalidData
	}

	return p, nil
}

func (r *memoryPropletRepo) Update(ctx context.Context, p proplet.Proplet) error {
	return r.storage.Update(ctx, p.ID, p)
}

func (r *memoryPropletRepo) List(ctx context.Context, offset, limit uint64) ([]proplet.Proplet, uint64, error) {
	data, total, err := r.storage.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	proplets := make([]proplet.Proplet, len(data))
	for i, d := range data {
		p, ok := d.(proplet.Proplet)
		if !ok {
			return nil, 0, pkgerrors.ErrInvalidData
		}
		proplets[i] = p
	}

	return proplets, total, nil
}

func (r *memoryPropletRepo) Delete(ctx context.Context, id string) error {
	return r.storage.Delete(ctx, id)
}

func (r *memoryPropletRepo) GetAliveHistory(ctx context.Context, id string, offset, limit uint64) ([]time.Time, uint64, error) {
	p, err := r.Get(ctx, id)
	if err != nil {
		return nil, 0, err
	}

	total := uint64(len(p.AliveHistory))
	if offset >= total {
		return []time.Time{}, total, nil
	}

	end := min(offset+limit, total)

	return p.AliveHistory[offset:end], total, nil
}

type memoryTaskPropletRepo struct {
	storage Storage
}

func newMemoryTaskPropletRepository(s Storage) TaskPropletRepository {
	return &memoryTaskPropletRepo{storage: s}
}

func (r *memoryTaskPropletRepo) Create(ctx context.Context, taskID, propletID string) error {
	return r.storage.Create(ctx, taskID, propletID)
}

func (r *memoryTaskPropletRepo) Get(ctx context.Context, taskID string) (string, error) {
	data, err := r.storage.Get(ctx, taskID)
	if err != nil {
		return "", err
	}
	propletID, ok := data.(string)
	if !ok {
		return "", pkgerrors.ErrInvalidData
	}

	return propletID, nil
}

func (r *memoryTaskPropletRepo) Delete(ctx context.Context, taskID string) error {
	return r.storage.Delete(ctx, taskID)
}

type memoryJobRepo struct {
	storage Storage
}

func newMemoryJobRepository(s Storage) JobRepository {
	return &memoryJobRepo{storage: s}
}

func (r *memoryJobRepo) Create(ctx context.Context, j job.Job) (job.Job, error) {
	if err := r.storage.Create(ctx, j.ID, j); err != nil {
		return job.Job{}, err
	}

	return j, nil
}

func (r *memoryJobRepo) Get(ctx context.Context, id string) (job.Job, error) {
	data, err := r.storage.Get(ctx, id)
	if err != nil {
		return job.Job{}, err
	}
	j, ok := data.(job.Job)
	if !ok {
		return job.Job{}, pkgerrors.ErrInvalidData
	}

	return j, nil
}

func (r *memoryJobRepo) List(ctx context.Context, offset, limit uint64) ([]job.Job, uint64, error) {
	data, total, err := r.storage.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	jobs := make([]job.Job, 0, len(data))
	for _, d := range data {
		j, ok := d.(job.Job)
		if !ok {
			continue
		}
		jobs = append(jobs, j)
	}

	return jobs, total, nil
}

func (r *memoryJobRepo) Delete(ctx context.Context, id string) error {
	return r.storage.Delete(ctx, id)
}

const maxMemoryFetch = 100000

type memoryMetricsRepo struct {
	storage Storage
}

func newMemoryMetricsRepository(s Storage) MetricsRepository {
	return &memoryMetricsRepo{storage: s}
}

func (r *memoryMetricsRepo) CreateTaskMetrics(ctx context.Context, m TaskMetrics) error {
	key := fmt.Sprintf("%s:%d", m.TaskID, m.Timestamp.UnixNano())

	return r.storage.Create(ctx, key, m)
}

func (r *memoryMetricsRepo) CreatePropletMetrics(ctx context.Context, m PropletMetrics) error {
	key := fmt.Sprintf("%s:%d", m.PropletID, m.Timestamp.UnixNano())

	return r.storage.Create(ctx, key, m)
}

func (r *memoryMetricsRepo) ListTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) ([]TaskMetrics, uint64, error) {
	var (
		scanOffset uint64
		total      uint64
		filtered   []TaskMetrics
	)

	for {
		data, allTotal, err := r.storage.List(ctx, scanOffset, memoryScanPageSize)
		if err != nil {
			return nil, 0, err
		}
		if len(data) == 0 {
			break
		}

		for _, d := range data {
			m, ok := d.(TaskMetrics)
			if !ok {
				continue
			}
			if m.TaskID != taskID {
				continue
			}

			if total >= offset && uint64(len(filtered)) < limit {
				filtered = append(filtered, m)
			}
			total++
		}

		scanOffset += uint64(len(data))
		if scanOffset >= allTotal {
			break
		}
	}

	if offset >= total {
		return []TaskMetrics{}, total, nil
	}

	return filtered, total, nil
}

func (r *memoryMetricsRepo) ListPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) ([]PropletMetrics, uint64, error) {
	var (
		scanOffset uint64
		total      uint64
		filtered   []PropletMetrics
	)

	for {
		data, allTotal, err := r.storage.List(ctx, scanOffset, memoryScanPageSize)
		if err != nil {
			return nil, 0, err
		}
		if len(data) == 0 {
			break
		}

		for _, d := range data {
			m, ok := d.(PropletMetrics)
			if !ok {
				continue
			}
			if m.PropletID != propletID {
				continue
			}

			if total >= offset && uint64(len(filtered)) < limit {
				filtered = append(filtered, m)
			}
			total++
		}

		scanOffset += uint64(len(data))
		if scanOffset >= allTotal {
			break
		}
	}

	if offset >= total {
		return []PropletMetrics{}, total, nil
	}

	return filtered, total, nil
}
