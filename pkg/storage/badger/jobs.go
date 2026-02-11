package badger

import (
	"context"
	"encoding/json"
	"fmt"
)

type jobRepo struct {
	db *Database
}

func NewJobRepository(db *Database) JobRepository {
	return &jobRepo{db: db}
}

func (r *jobRepo) Create(ctx context.Context, j Job) (Job, error) {
	key := []byte("job:" + j.ID)
	val, err := json.Marshal(j)
	if err != nil {
		return Job{}, fmt.Errorf("marshal error: %w", err)
	}
	if err := r.db.set(key, val); err != nil {
		return Job{}, fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return j, nil
}

func (r *jobRepo) Get(ctx context.Context, id string) (Job, error) {
	key := []byte("job:" + id)
	val, err := r.db.get(key)
	if err != nil {
		return Job{}, ErrNotFound
	}
	var j Job
	if err := json.Unmarshal(val, &j); err != nil {
		return Job{}, fmt.Errorf("unmarshal error: %w", err)
	}

	return j, nil
}

func (r *jobRepo) List(ctx context.Context, offset, limit uint64) ([]Job, uint64, error) {
	prefix := []byte("job:")
	total, err := r.db.countWithPrefix(prefix)
	if err != nil {
		return nil, 0, err
	}
	values, err := r.db.listWithPrefix(prefix, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	jobs := make([]Job, len(values))
	for i, val := range values {
		var j Job
		if err := json.Unmarshal(val, &j); err != nil {
			return nil, 0, fmt.Errorf("unmarshal error: %w", err)
		}
		jobs[i] = j
	}

	return jobs, total, nil
}

func (r *jobRepo) Delete(ctx context.Context, id string) error {
	key := []byte("job:" + id)

	return r.db.delete(key)
}
