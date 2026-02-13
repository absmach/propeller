package badger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/task"
	"github.com/dgraph-io/badger/v4"
)

var (
	ErrDBConnection    = errors.New("badger database connection error")
	ErrDBQuery         = errors.New("database query error")
	ErrDBScan          = errors.New("database scan error")
	ErrCreate          = errors.New("create error")
	ErrUpdate          = errors.New("update error")
	ErrDelete          = errors.New("delete error")
	ErrTaskNotFound    = errors.New("task not found")
	ErrPropletNotFound = errors.New("proplet not found")
	ErrNotFound        = errors.New("not found")
)

type TaskMetrics struct {
	TaskID     string                     `json:"task_id"`
	PropletID  string                     `json:"proplet_id"`
	Metrics    proplet.ProcessMetrics     `json:"metrics"`
	Aggregated *proplet.AggregatedMetrics `json:"aggregated,omitempty"`
	Timestamp  time.Time                  `json:"timestamp"`
}

type PropletMetrics struct {
	PropletID string                `json:"proplet_id"`
	Namespace string                `json:"namespace"`
	Timestamp time.Time             `json:"timestamp"`
	CPU       proplet.CPUMetrics    `json:"cpu_metrics"`
	Memory    proplet.MemoryMetrics `json:"memory_metrics"`
}

type TaskRepository interface {
	Create(ctx context.Context, t task.Task) (task.Task, error)
	Get(ctx context.Context, id string) (task.Task, error)
	Update(ctx context.Context, t task.Task) error
	List(ctx context.Context, offset, limit uint64) ([]task.Task, uint64, error)
	ListByWorkflowID(ctx context.Context, workflowID string) ([]task.Task, error)
	ListByJobID(ctx context.Context, jobID string) ([]task.Task, error)
	Delete(ctx context.Context, id string) error
}

type Job struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	ExecutionMode string    `json:"execution_mode"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type JobRepository interface {
	Create(ctx context.Context, j Job) (Job, error)
	Get(ctx context.Context, id string) (Job, error)
	List(ctx context.Context, offset, limit uint64) ([]Job, uint64, error)
	Delete(ctx context.Context, id string) error
}

type PropletRepository interface {
	Create(ctx context.Context, p proplet.Proplet) error
	Get(ctx context.Context, id string) (proplet.Proplet, error)
	Update(ctx context.Context, p proplet.Proplet) error
	List(ctx context.Context, offset, limit uint64) ([]proplet.Proplet, uint64, error)
	Delete(ctx context.Context, id string) error
}

type TaskPropletRepository interface {
	Create(ctx context.Context, taskID, propletID string) error
	Get(ctx context.Context, taskID string) (string, error)
	Delete(ctx context.Context, taskID string) error
}

type MetricsRepository interface {
	CreateTaskMetrics(ctx context.Context, m TaskMetrics) error
	CreatePropletMetrics(ctx context.Context, m PropletMetrics) error
	ListTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) ([]TaskMetrics, uint64, error)
	ListPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) ([]PropletMetrics, uint64, error)
}

type Repositories struct {
	Tasks        TaskRepository
	Proplets     PropletRepository
	TaskProplets TaskPropletRepository
	Jobs         JobRepository
	Metrics      MetricsRepository
}

func NewRepositories(db *Database) *Repositories {
	return &Repositories{
		Tasks:        NewTaskRepository(db),
		Proplets:     NewPropletRepository(db),
		TaskProplets: NewTaskPropletRepository(db),
		Jobs:         NewJobRepository(db),
		Metrics:      NewMetricsRepository(db),
	}
}

type Database struct {
	db *badger.DB
}

func NewDatabase(path string) (*Database, error) {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDBConnection, err)
	}

	return &Database{db: db}, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) get(key []byte) ([]byte, error) {
	var val []byte
	err := d.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		val, err = item.ValueCopy(nil)

		return err
	})
	if err != nil {
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil, ErrNotFound
		}

		return nil, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return val, nil
}

func (d *Database) set(key, val []byte) error {
	err := d.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, val)
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUpdate, err)
	}

	return nil
}

func (d *Database) delete(key []byte) error {
	err := d.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDelete, err)
	}

	return nil
}

func (d *Database) listWithPrefix(prefix []byte, offset, limit uint64) ([][]byte, error) {
	var items [][]byte
	err := d.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = int(limit)
		it := txn.NewIterator(opts)
		defer it.Close()

		skipped := uint64(0)
		count := uint64(0)

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			if skipped < offset {
				skipped++

				continue
			}
			if count >= limit {
				break
			}

			item := it.Item()
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			items = append(items, val)
			count++
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return items, nil
}

func (d *Database) countWithPrefix(prefix []byte) (uint64, error) {
	count := uint64(0)
	err := d.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			count++
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return count, nil
}
