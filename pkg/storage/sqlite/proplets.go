package sqlite

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
	query := `INSERT INTO proplets (id, name, task_count, alive, alive_history, metadata) VALUES (?, ?, ?, ?, ?, ?)`

	aliveHistory, err := jsonBytes(p.AliveHistory)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	metadata, err := jsonBytes(p.Metadata)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	_, err = r.db.ExecContext(ctx, query, p.ID, p.Name, p.TaskCount, p.Alive, aliveHistory, metadata)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return nil
}

func (r *propletRepo) Get(ctx context.Context, id string) (proplet.Proplet, error) {
	query := `SELECT id, name, task_count, alive, alive_history, metadata FROM proplets WHERE id = ?`

	var dbp dbProplet
	err := r.db.GetContext(ctx, &dbp, query, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return proplet.Proplet{}, ErrPropletNotFound
		}

		return proplet.Proplet{}, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return r.toProplet(dbp)
}

func (r *propletRepo) Update(ctx context.Context, p proplet.Proplet) error {
	query := `UPDATE proplets SET name = ?, task_count = ?, alive = ?, alive_history = ?, metadata = ? WHERE id = ?`

	aliveHistory, err := jsonBytes(p.AliveHistory)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	metadata, err := jsonBytes(p.Metadata)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	if _, err = r.db.ExecContext(ctx, query, p.Name, p.TaskCount, p.Alive, aliveHistory, metadata, p.ID); err != nil {
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

	query := `SELECT id, name, task_count, alive, alive_history, metadata FROM proplets LIMIT ? OFFSET ?`

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
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}
	defer func() { _ = tx.Rollback() }()

	sinceArg := since.Format(time.RFC3339Nano)

	// Use '$[' || (json_array_length-1) || ']' instead of '$[#-1]' to support
	// SQLite < 3.38.0 (the last-element shorthand was added in 3.38, Feb 2022).
	// Use strftime('%s',...) instead of unixepoch() for the same reason: unixepoch()
	// was also added in 3.38.0, while strftime has been available since SQLite 2.8.
	whereClause := `WHERE alive_history IS NOT NULL AND json_array_length(alive_history) > 0 AND CAST(strftime('%s', json_extract(alive_history, '$[' || (json_array_length(alive_history) - 1) || ']')) AS INTEGER) >= CAST(strftime('%s', ?) AS INTEGER)`
	if !alive {
		whereClause = `WHERE alive_history IS NULL OR json_array_length(alive_history) = 0 OR CAST(strftime('%s', json_extract(alive_history, '$[' || (json_array_length(alive_history) - 1) || ']')) AS INTEGER) < CAST(strftime('%s', ?) AS INTEGER)`
	}

	var total uint64
	if err := tx.GetContext(ctx, &total, "SELECT COUNT(*) FROM proplets "+whereClause, sinceArg); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	query := `SELECT id, name, task_count, alive, alive_history, metadata FROM proplets ` + whereClause + ` LIMIT ? OFFSET ?`
	rows, err := tx.QueryContext(ctx, query, sinceArg, limit, offset)
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
	query := `DELETE FROM proplets WHERE id = ?`

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
