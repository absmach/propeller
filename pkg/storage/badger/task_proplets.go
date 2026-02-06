package badger

import (
	"context"
	"fmt"
)

type taskPropletRepo struct {
	db *Database
}

func NewTaskPropletRepository(db *Database) TaskPropletRepository {
	return &taskPropletRepo{db: db}
}

func (r *taskPropletRepo) Create(ctx context.Context, taskID, propletID string) error {
	key := []byte("map:task:" + taskID)
	if err := r.db.set(key, []byte(propletID)); err != nil {
		return fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return nil
}

func (r *taskPropletRepo) Get(ctx context.Context, taskID string) (string, error) {
	key := []byte("map:task:" + taskID)
	val, err := r.db.get(key)
	if err != nil {
		return "", ErrNotFound
	}

	return string(val), nil
}

func (r *taskPropletRepo) Delete(ctx context.Context, taskID string) error {
	key := []byte("map:task:" + taskID)

	return r.db.delete(key)
}
