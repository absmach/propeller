package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type taskPropletRepo struct {
	db *Database
}

func NewTaskPropletRepository(db *Database) TaskPropletRepository {
	return &taskPropletRepo{db: db}
}

func (r *taskPropletRepo) Create(ctx context.Context, taskID, propletID string) error {
	query := `INSERT INTO task_proplets (task_id, proplet_id) VALUES ($1, $2)`

	if _, err := r.db.ExecContext(ctx, query, taskID, propletID); err != nil {
		return fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return nil
}

func (r *taskPropletRepo) Get(ctx context.Context, taskID string) (string, error) {
	query := `SELECT proplet_id FROM task_proplets WHERE task_id = $1`

	var propletID string

	if err := r.db.GetContext(ctx, &propletID, query, taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}

		return "", fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return propletID, nil
}

func (r *taskPropletRepo) Delete(ctx context.Context, taskID string) error {
	query := `DELETE FROM task_proplets WHERE task_id = $1`

	if _, err := r.db.ExecContext(ctx, query, taskID); err != nil {
		return fmt.Errorf("%w: %w", ErrDelete, err)
	}

	return nil
}
