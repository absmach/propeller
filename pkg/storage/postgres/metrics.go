package postgres

import (
	"context"
	"fmt"
	"time"
)

type metricsRepo struct {
	db *Database
}

func NewMetricsRepository(db *Database) MetricsRepository {
	return &metricsRepo{db: db}
}

type dbTaskMetrics struct {
	ID         string    `db:"id"`
	TaskID     string    `db:"task_id"`
	PropletID  string    `db:"proplet_id"`
	Metrics    []byte    `db:"metrics"`
	Aggregated []byte    `db:"aggregated"`
	Timestamp  time.Time `db:"timestamp"`
}

type dbPropletMetrics struct {
	ID            string    `db:"id"`
	PropletID     string    `db:"proplet_id"`
	Namespace     string    `db:"namespace"`
	CPUMetrics    []byte    `db:"cpu_metrics"`
	MemoryMetrics []byte    `db:"memory_metrics"`
	Timestamp     time.Time `db:"timestamp"`
}

func (r *metricsRepo) CreateTaskMetrics(ctx context.Context, m TaskMetrics) error {
	query := `INSERT INTO task_metrics (id, task_id, proplet_id, metrics, aggregated, timestamp) VALUES ($1, $2, $3, $4, $5, $6)`

	metrics, err := jsonBytes(m.Metrics)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	aggregated, err := jsonBytes(m.Aggregated)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	id := fmt.Sprintf("%s:%d", m.TaskID, m.Timestamp.UnixNano())

	if _, err = r.db.ExecContext(ctx, query, id, m.TaskID, m.PropletID, metrics, aggregated, m.Timestamp); err != nil {
		return fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return nil
}

func (r *metricsRepo) CreatePropletMetrics(ctx context.Context, m PropletMetrics) error {
	query := `INSERT INTO proplet_metrics (id, proplet_id, namespace, cpu_metrics, memory_metrics, timestamp) VALUES ($1, $2, $3, $4, $5, $6)`

	cpuMetrics, err := jsonBytes(m.CPU)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	memoryMetrics, err := jsonBytes(m.Memory)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	id := fmt.Sprintf("%s:%d", m.PropletID, m.Timestamp.UnixNano())

	if _, err = r.db.ExecContext(ctx, query, id, m.PropletID, m.Namespace, cpuMetrics, memoryMetrics, m.Timestamp); err != nil {
		return fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return nil
}

func (r *metricsRepo) ListTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) ([]TaskMetrics, uint64, error) {
	query := `SELECT COUNT(*) FROM task_metrics WHERE task_id = $1`

	var total uint64
	err := r.db.GetContext(ctx, &total, query, taskID)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	query = `SELECT id, task_id, proplet_id, metrics, aggregated, timestamp FROM task_metrics WHERE task_id = $1 ORDER BY timestamp DESC LIMIT $2 OFFSET $3`

	rows, err := r.db.QueryContext(ctx, query, taskID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}
	defer rows.Close()

	metrics := make([]TaskMetrics, 0)
	for rows.Next() {
		var dbm dbTaskMetrics
		if err := rows.Scan(&dbm.ID, &dbm.TaskID, &dbm.PropletID, &dbm.Metrics, &dbm.Aggregated, &dbm.Timestamp); err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrDBScan, err)
		}

		m, err := r.toTaskMetrics(dbm)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrDBScan, err)
		}

		metrics = append(metrics, m)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return metrics, total, nil
}

func (r *metricsRepo) ListPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) ([]PropletMetrics, uint64, error) {
	query := `SELECT COUNT(*) FROM proplet_metrics WHERE proplet_id = $1`

	var total uint64
	err := r.db.GetContext(ctx, &total, query, propletID)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	query = `SELECT id, proplet_id, namespace, cpu_metrics, memory_metrics, timestamp FROM proplet_metrics WHERE proplet_id = $1 ORDER BY timestamp DESC LIMIT $2 OFFSET $3`

	rows, err := r.db.QueryContext(ctx, query, propletID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}
	defer rows.Close()

	metrics := make([]PropletMetrics, 0)
	for rows.Next() {
		var dbm dbPropletMetrics
		if err := rows.Scan(&dbm.ID, &dbm.PropletID, &dbm.Namespace, &dbm.CPUMetrics, &dbm.MemoryMetrics, &dbm.Timestamp); err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrDBScan, err)
		}

		m, err := r.toPropletMetrics(dbm)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrDBScan, err)
		}

		metrics = append(metrics, m)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return metrics, total, nil
}

func (r *metricsRepo) toTaskMetrics(dbm dbTaskMetrics) (TaskMetrics, error) {
	m := TaskMetrics{
		TaskID:    dbm.TaskID,
		PropletID: dbm.PropletID,
		Timestamp: dbm.Timestamp,
	}

	if dbm.Metrics != nil {
		if err := jsonUnmarshal(dbm.Metrics, &m.Metrics); err != nil {
			return TaskMetrics{}, err
		}
	}
	if dbm.Aggregated != nil {
		if err := jsonUnmarshal(dbm.Aggregated, &m.Aggregated); err != nil {
			return TaskMetrics{}, err
		}
	}

	return m, nil
}

func (r *metricsRepo) toPropletMetrics(dbm dbPropletMetrics) (PropletMetrics, error) {
	m := PropletMetrics{
		PropletID: dbm.PropletID,
		Namespace: dbm.Namespace,
		Timestamp: dbm.Timestamp,
	}

	if dbm.CPUMetrics != nil {
		if err := jsonUnmarshal(dbm.CPUMetrics, &m.CPU); err != nil {
			return PropletMetrics{}, err
		}
	}
	if dbm.MemoryMetrics != nil {
		if err := jsonUnmarshal(dbm.MemoryMetrics, &m.Memory); err != nil {
			return PropletMetrics{}, err
		}
	}

	return m, nil
}
