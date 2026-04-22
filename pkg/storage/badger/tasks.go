package badger

import (
	"context"
	"encoding/json"
	"errors"
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

func metaIdxKey(k, v, id string) []byte {
	return []byte("meta-idx\x00" + k + "\x00" + v + "\x00" + id)
}

func (r *taskRepo) Create(ctx context.Context, t task.Task) (task.Task, error) {
	key := []byte("task:" + t.ID)
	val, err := json.Marshal(t)
	if err != nil {
		return task.Task{}, fmt.Errorf("marshal error: %w", err)
	}
	err = r.db.updateTxn(func(txn *badgerdb.Txn) error {
		if err := txn.Set(key, val); err != nil {
			return err
		}

		return r.indexTaskTxn(txn, t)
	})
	if err != nil {
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
	err = r.db.updateTxn(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return ErrTaskNotFound
		}

		oldVal, err := item.ValueCopy(nil)
		if err == nil {
			var old task.Task
			if err := json.Unmarshal(oldVal, &old); err == nil {
				if err := r.deindexTaskTxn(txn, old); err != nil {
					return err
				}
			}
		}

		if err := txn.Set(key, val); err != nil {
			return err
		}

		return r.indexTaskTxn(txn, t)
	})
	if err != nil {
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

func (r *taskRepo) ListByMetadataFilter(ctx context.Context, filter task.Metadata, offset, limit uint64) ([]task.Task, uint64, error) {
	if len(filter) == 0 {
		return r.List(ctx, offset, limit)
	}

	var matchIDs map[string]struct{}
	err := r.db.viewTxn(func(txn *badgerdb.Txn) error {
		opts := badgerdb.DefaultIteratorOptions
		opts.PrefetchValues = false
		for k, vAny := range filter {
			v, ok := vAny.(string)
			if !ok {
				continue
			}
			prefix := []byte("meta-idx\x00" + k + "\x00" + v + "\x00")
			it := txn.NewIterator(opts)
			ids := make(map[string]struct{})
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				key := it.Item().KeyCopy(nil)
				taskID := string(key[len(prefix):])
				ids[taskID] = struct{}{}
			}
			it.Close()

			if matchIDs == nil {
				matchIDs = ids
			} else {
				for id := range matchIDs {
					if _, ok := ids[id]; !ok {
						delete(matchIDs, id)
					}
				}
			}
			if len(matchIDs) == 0 {
				return nil
			}
		}

		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	if len(matchIDs) == 0 {
		return []task.Task{}, 0, nil
	}

	tasks := make([]task.Task, 0, len(matchIDs))
	for id := range matchIDs {
		t, err := r.Get(ctx, id)
		if err != nil {
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
	err := r.db.updateTxn(func(txn *badgerdb.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return txn.Delete(key)
		}

		val, err := item.ValueCopy(nil)
		if err != nil {
			return txn.Delete(key)
		}

		var t task.Task
		if err := json.Unmarshal(val, &t); err == nil {
			if err := r.deindexTaskTxn(txn, t); err != nil {
				return err
			}
		}

		return txn.Delete(key)
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDelete, err)
	}

	return nil
}

func (r *taskRepo) indexTaskTxn(txn *badgerdb.Txn, t task.Task) error {
	for k, v := range t.Metadata {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if err := txn.Set(metaIdxKey(k, s, t.ID), []byte{}); err != nil {
			return err
		}
	}

	return nil
}

func (r *taskRepo) deindexTaskTxn(txn *badgerdb.Txn, t task.Task) error {
	for k, v := range t.Metadata {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if err := txn.Delete(metaIdxKey(k, s, t.ID)); err != nil && !errors.Is(err, badgerdb.ErrKeyNotFound) {
			return err
		}
	}

	return nil
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
