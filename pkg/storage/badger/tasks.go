package badger

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/absmach/propeller/task"
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

func (r *taskRepo) Delete(ctx context.Context, id string) error {
	key := []byte("task:" + id)

	return r.db.delete(key)
}
