package badger

import (
	"context"
	"encoding/json"
	"fmt"
)

type metricsRepo struct {
	db *Database
}

func NewMetricsRepository(db *Database) MetricsRepository {
	return &metricsRepo{db: db}
}

func (r *metricsRepo) CreateTaskMetrics(ctx context.Context, m TaskMetrics) error {
	key := fmt.Appendf([]byte{}, "tm:%s:%d", m.TaskID, m.Timestamp.UnixNano())
	val, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}
	if err := r.db.set(key, val); err != nil {
		return fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return nil
}

func (r *metricsRepo) CreatePropletMetrics(ctx context.Context, m PropletMetrics) error {
	key := fmt.Appendf([]byte{}, "pm:%s:%d", m.PropletID, m.Timestamp.UnixNano())
	val, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}
	if err := r.db.set(key, val); err != nil {
		return fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return nil
}

func (r *metricsRepo) ListTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) ([]TaskMetrics, uint64, error) {
	prefix := []byte("tm:" + taskID)
	total, err := r.db.countWithPrefix(prefix)
	if err != nil {
		return nil, 0, err
	}
	values, err := r.db.listWithPrefix(prefix, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	metrics := make([]TaskMetrics, len(values))
	for i, val := range values {
		var m TaskMetrics
		if err := json.Unmarshal(val, &m); err != nil {
			return nil, 0, fmt.Errorf("unmarshal error: %w", err)
		}
		metrics[i] = m
	}

	return metrics, total, nil
}

func (r *metricsRepo) ListPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) ([]PropletMetrics, uint64, error) {
	prefix := []byte("pm:" + propletID)
	total, err := r.db.countWithPrefix(prefix)
	if err != nil {
		return nil, 0, err
	}
	values, err := r.db.listWithPrefix(prefix, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	metrics := make([]PropletMetrics, len(values))
	for i, val := range values {
		var m PropletMetrics
		if err := json.Unmarshal(val, &m); err != nil {
			return nil, 0, fmt.Errorf("unmarshal error: %w", err)
		}
		metrics[i] = m
	}

	return metrics, total, nil
}
