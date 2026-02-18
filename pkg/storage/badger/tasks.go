package badger

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/absmach/propeller/pkg/task"
)

type taskRepo struct {
	db *Database
}

func NewTaskRepository(db *Database) TaskRepository {
	return &taskRepo{db: db}
}

func (r *taskRepo) Create(ctx context.Context, t task.Task) (task.Task, error) {
	key := []byte("task:" + t.ID)
	val, err := json.Marshal(t)
	if err != nil {
		return task.Task{}, fmt.Errorf("marshal error: %w", err)
	}
	if err := r.db.set(key, val); err != nil {
		return task.Task{}, fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return t, nil
}

func (r *taskRepo) Get(ctx context.Context, id string) (task.Task, error) {
	key := []byte("task:" + id)
	val, err := r.db.get(key)
	if err != nil {
		return task.Task{}, ErrTaskNotFound
	}
	var t task.Task
	if err := json.Unmarshal(val, &t); err != nil {
		return task.Task{}, fmt.Errorf("unmarshal error: %w", err)
	}

	return t, nil
}

func (r *taskRepo) Update(ctx context.Context, t task.Task) error {
	key := []byte("task:" + t.ID)
	val, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}
	if err := r.db.set(key, val); err != nil {
		return fmt.Errorf("%w: %w", ErrUpdate, err)
	}

	return nil
}

func (r *taskRepo) List(ctx context.Context, offset, limit uint64) ([]task.Task, uint64, error) {
	prefix := []byte("task:")
	total, err := r.db.countWithPrefix(prefix)
	if err != nil {
		return nil, 0, err
	}
	values, err := r.db.listWithPrefix(prefix, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	tasks := make([]task.Task, len(values))
	for i, val := range values {
		var t task.Task
		if err := json.Unmarshal(val, &t); err != nil {
			return nil, 0, fmt.Errorf("unmarshal error: %w", err)
		}
		tasks[i] = t
	}

	return tasks, total, nil
}

func (r *taskRepo) ListByWorkflowID(_ context.Context, workflowID string, offset, limit uint64) ([]task.Task, uint64, error) {
	// Badger is a key-value store with no secondary indexes. We scan all tasks
	// by the "task:" prefix, deserialize each one, and filter by WorkflowID.
	// This is O(N) over total tasks but is bounded by the per-page limit when
	// building the response slice.
	prefix := []byte("task:")
	allValues, err := r.db.listWithPrefix(prefix, 0, ^uint64(0))
	if err != nil {
		return nil, 0, err
	}

	var filtered []task.Task
	for _, val := range allValues {
		var t task.Task
		if err := json.Unmarshal(val, &t); err != nil {
			return nil, 0, fmt.Errorf("unmarshal error: %w", err)
		}
		if t.WorkflowID == workflowID {
			filtered = append(filtered, t)
		}
	}

	total := uint64(len(filtered))
	if offset >= total {
		return []task.Task{}, total, nil
	}
	end := min(offset+limit, total)

	return filtered[offset:end], total, nil
}

func (r *taskRepo) Delete(ctx context.Context, id string) error {
	key := []byte("task:" + id)

	return r.db.delete(key)
}
