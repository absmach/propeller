package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/absmach/propeller/pkg/proplet"
)

type propletRepo struct {
	db *Database
}

func NewPropletRepository(db *Database) PropletRepository {
	return &propletRepo{db: db}
}

type dbProplet struct {
	ID           string `db:"id"`
	Name         string `db:"name"`
	TaskCount    uint64 `db:"task_count"`
	Alive        bool   `db:"alive"`
	AliveHistory []byte `db:"alive_history"`
	Metadata     []byte `db:"metadata"`
}

func (r *propletRepo) Create(ctx context.Context, p proplet.Proplet) error {
	query := `INSERT INTO proplets (id, name, task_count, alive, alive_history, metadata) VALUES ($1, $2, $3, $4, $5, $6)`

	aliveHistory, err := jsonBytes(p.AliveHistory)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	metadata, err := jsonBytes(p.Metadata)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	if _, err = r.db.ExecContext(ctx, query, p.ID, p.Name, p.TaskCount, p.Alive, aliveHistory, metadata); err != nil {
		return fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return nil
}

func (r *propletRepo) Get(ctx context.Context, id string) (proplet.Proplet, error) {
	query := `SELECT id, name, task_count, alive, alive_history, metadata FROM proplets WHERE id = $1`

	var dbp dbProplet

	if err := r.db.GetContext(ctx, &dbp, query, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return proplet.Proplet{}, ErrPropletNotFound
		}

		return proplet.Proplet{}, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return r.toProplet(dbp)
}

func (r *propletRepo) Update(ctx context.Context, p proplet.Proplet) error {
	query := `UPDATE proplets SET name = $2, task_count = $3, alive = $4, alive_history = $5, metadata = $6 WHERE id = $1`

	aliveHistory, err := jsonBytes(p.AliveHistory)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	metadata, err := jsonBytes(p.Metadata)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	if _, err = r.db.ExecContext(ctx, query, p.ID, p.Name, p.TaskCount, p.Alive, aliveHistory, metadata); err != nil {
		return fmt.Errorf("%w: %w", ErrUpdate, err)
	}

	return nil
}

func (r *propletRepo) List(ctx context.Context, offset, limit uint64) ([]proplet.Proplet, uint64, error) {
	var total uint64
	err := r.db.GetContext(ctx, &total, "SELECT COUNT(*) FROM proplets")
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	query := `SELECT id, name, task_count, alive, alive_history, metadata FROM proplets LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}
	defer rows.Close()

	proplets := make([]proplet.Proplet, 0)
	for rows.Next() {
		var dbp dbProplet
		if err := rows.Scan(&dbp.ID, &dbp.Name, &dbp.TaskCount, &dbp.Alive, &dbp.AliveHistory, &dbp.Metadata); err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrDBScan, err)
		}

		p, err := r.toProplet(dbp)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrDBScan, err)
		}

		proplets = append(proplets, p)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return proplets, total, nil
}

func (r *propletRepo) ListByAlive(ctx context.Context, offset, limit uint64, alive bool, since time.Time) ([]proplet.Proplet, uint64, error) {
	var whereClause string
	if alive {
		whereClause = `WHERE alive_history IS NOT NULL AND jsonb_array_length(alive_history) > 0 AND (alive_history ->> (jsonb_array_length(alive_history) - 1))::timestamptz >= $1`
	} else {
		whereClause = `WHERE alive_history IS NULL OR jsonb_array_length(alive_history) = 0 OR (alive_history ->> (jsonb_array_length(alive_history) - 1))::timestamptz < $1`
	}

	tx, err := r.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}
	defer func() { _ = tx.Rollback() }()

	var total uint64
	if err := tx.GetContext(ctx, &total, "SELECT COUNT(*) FROM proplets "+whereClause, since); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	query := fmt.Sprintf(`SELECT id, name, task_count, alive, alive_history, metadata FROM proplets %s LIMIT $2 OFFSET $3`, whereClause)
	rows, err := tx.QueryContext(ctx, query, since, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}
	defer rows.Close()

	proplets := make([]proplet.Proplet, 0)
	for rows.Next() {
		var dbp dbProplet
		if err := rows.Scan(&dbp.ID, &dbp.Name, &dbp.TaskCount, &dbp.Alive, &dbp.AliveHistory, &dbp.Metadata); err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrDBScan, err)
		}
		p, err := r.toProplet(dbp)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrDBScan, err)
		}
		proplets = append(proplets, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return proplets, total, nil
}

func (r *propletRepo) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM proplets WHERE id = $1`

	if _, err := r.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("%w: %w", ErrDelete, err)
	}

	return nil
}

func (r *propletRepo) GetAliveHistory(ctx context.Context, id string, offset, limit uint64) ([]time.Time, uint64, error) {
	p, err := r.Get(ctx, id)
	if err != nil {
		return nil, 0, err
	}

	total := uint64(len(p.AliveHistory))
	if offset >= total {
		return []time.Time{}, total, nil
	}

	end := min(offset+limit, total)

	return p.AliveHistory[offset:end], total, nil
}

func (r *propletRepo) toProplet(dbp dbProplet) (proplet.Proplet, error) {
	p := proplet.Proplet{
		ID:        dbp.ID,
		Name:      dbp.Name,
		TaskCount: dbp.TaskCount,
		Alive:     dbp.Alive,
	}

	if dbp.AliveHistory != nil {
		if err := jsonUnmarshal(dbp.AliveHistory, &p.AliveHistory); err != nil {
			return proplet.Proplet{}, err
		}
	}

	if dbp.Metadata != nil {
		if err := jsonUnmarshal(dbp.Metadata, &p.Metadata); err != nil {
			return proplet.Proplet{}, err
		}
	}

	return p, nil
}
