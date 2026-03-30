package badger

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/absmach/propeller/pkg/task"
	badgerdb "github.com/dgraph-io/badger/v4"
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

func (r *taskRepo) ListByMetadataFilter(ctx context.Context, filter map[string]string, offset, limit uint64) ([]task.Task, uint64, error) {
	all, err := r.listBy(ctx, func(t task.Task) bool {
		if t.Metadata == nil {
			return false
		}
		for k, v := range filter {
			val, exists := t.Metadata[k]
			if !exists {
				return false
			}
			s, ok := val.(string)
			if !ok || s != v {
				return false
			}
		}

		return true
	})
	if err != nil {
		return nil, 0, err
	}
	total := uint64(len(all))
	if offset >= total {
		return []task.Task{}, total, nil
	}
	end := min(offset+limit, total)

	return all[offset:end], total, nil
}

func (r *taskRepo) ListByWorkflowID(ctx context.Context, workflowID string) ([]task.Task, error) {
	return r.listBy(ctx, func(t task.Task) bool {
		return t.WorkflowID == workflowID
	})
}

func (r *taskRepo) ListByJobID(ctx context.Context, jobID string) ([]task.Task, error) {
	return r.listBy(ctx, func(t task.Task) bool {
		return t.JobID == jobID
	})
}

func (r *taskRepo) Delete(ctx context.Context, id string) error {
	key := []byte("task:" + id)

	return r.db.delete(key)
}

func (r *taskRepo) listBy(ctx context.Context, match func(task.Task) bool) ([]task.Task, error) {
	prefix := []byte("task:")
	tasks := make([]task.Task, 0)

	err := r.db.db.View(func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			val, err := it.Item().ValueCopy(nil)
			if err != nil {
				return err
			}

			var t task.Task
			if err := json.Unmarshal(val, &t); err != nil {
				return fmt.Errorf("unmarshal error: %w", err)
			}

			if match(t) {
				tasks = append(tasks, t)
			}
		}

		return nil
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		return nil, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return tasks, nil
}
