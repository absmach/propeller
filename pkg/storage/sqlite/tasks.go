package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/absmach/propeller/task"
)

type taskRepo struct {
	db *Database
}

func NewTaskRepository(db *Database) TaskRepository {
	return &taskRepo{db: db}
}

type dbTask struct {
	ID                string       `db:"id"`
	Name              string       `db:"name"`
	State             uint8        `db:"state"`
	ImageURL          *string      `db:"image_url"`
	File              []byte       `db:"file"`
	CLIArgs           []byte       `db:"cli_args"`
	Inputs            []byte       `db:"inputs"`
	Env               []byte       `db:"env"`
	Daemon            bool         `db:"daemon"`
	Encrypted         bool         `db:"encrypted"`
	KBSResourcePath   *string      `db:"kbs_resource_path"`
	PropletID         *string      `db:"proplet_id"`
	Results           []byte       `db:"results"`
	Error             *string      `db:"error"`
	MonitoringProfile []byte       `db:"monitoring_profile"`
	StartTime         sql.NullTime `db:"start_time"`
	FinishTime        sql.NullTime `db:"finish_time"`
	CreatedAt         sql.NullTime `db:"created_at"`
	UpdatedAt         sql.NullTime `db:"updated_at"`
}

func (r *taskRepo) Create(ctx context.Context, t task.Task) (task.Task, error) {
	query := `INSERT INTO tasks (id, name, state, image_url, file, cli_args, inputs, env, daemon, encrypted, kbs_resource_path, proplet_id, results, error, monitoring_profile, start_time, finish_time, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	cliArgs, err := jsonBytes(t.CLIArgs)
	if err != nil {
		return task.Task{}, fmt.Errorf("marshal error: %w", err)
	}

	inputs, err := jsonBytes(t.Inputs)
	if err != nil {
		return task.Task{}, fmt.Errorf("marshal error: %w", err)
	}

	env, err := jsonBytes(t.Env)
	if err != nil {
		return task.Task{}, fmt.Errorf("marshal error: %w", err)
	}

	results, err := jsonBytes(t.Results)
	if err != nil {
		return task.Task{}, fmt.Errorf("marshal error: %w", err)
	}

	monitoringProfile, err := jsonBytes(t.MonitoringProfile)
	if err != nil {
		return task.Task{}, fmt.Errorf("marshal error: %w", err)
	}

	_, err = r.db.ExecContext(ctx, query,
		t.ID, t.Name, uint8(t.State), nullString(t.ImageURL),
		t.File, cliArgs, inputs, env,
		t.Daemon, t.Encrypted, nullString(t.KBSResourcePath),
		nullString(t.PropletID), results, nullString(t.Error),
		monitoringProfile, nullTime(t.StartTime), nullTime(t.FinishTime),
		t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return task.Task{}, fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return t, nil
}

func (r *taskRepo) Get(ctx context.Context, id string) (task.Task, error) {
	query := `SELECT id, name, state, image_url, file, cli_args, inputs, env, daemon, encrypted, kbs_resource_path, proplet_id, results, error, monitoring_profile, start_time, finish_time, created_at, updated_at
		FROM tasks WHERE id = ?`

	var dbt dbTask
	err := r.db.GetContext(ctx, &dbt, query, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return task.Task{}, ErrTaskNotFound
		}

		return task.Task{}, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return r.toTask(dbt)
}

func (r *taskRepo) Update(ctx context.Context, t task.Task) error {
	query := `UPDATE tasks SET
		name = ?,
		state = ?,
		image_url = ?,
		file = ?,
		cli_args = ?,
		inputs = ?,
		env = ?,
		daemon = ?,
		encrypted = ?,
		kbs_resource_path = ?,
		proplet_id = ?,
		results = ?,
		error = ?,
		monitoring_profile = ?,
		start_time = ?,
		finish_time = ?,
		updated_at = ?
	WHERE id = ?`

	cliArgs, err := jsonBytes(t.CLIArgs)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	inputs, err := jsonBytes(t.Inputs)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	env, err := jsonBytes(t.Env)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	results, err := jsonBytes(t.Results)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	monitoringProfile, err := jsonBytes(t.MonitoringProfile)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	_, err = r.db.ExecContext(ctx, query,
		t.Name, uint8(t.State), nullString(t.ImageURL),
		t.File, cliArgs, inputs, env,
		t.Daemon, t.Encrypted, nullString(t.KBSResourcePath),
		nullString(t.PropletID), results, nullString(t.Error),
		monitoringProfile, nullTime(t.StartTime), nullTime(t.FinishTime),
		t.UpdatedAt, t.ID,
	)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUpdate, err)
	}

	return nil
}

func (r *taskRepo) List(ctx context.Context, offset, limit uint64) ([]task.Task, uint64, error) {
	var total uint64
	err := r.db.GetContext(ctx, &total, "SELECT COUNT(*) FROM tasks")
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	query := `SELECT id, name, state, image_url, file, cli_args, inputs, env, daemon, encrypted, kbs_resource_path, proplet_id, results, error, monitoring_profile, start_time, finish_time, created_at, updated_at
		FROM tasks ORDER BY created_at DESC LIMIT ? OFFSET ?`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}
	defer rows.Close()

	tasks := make([]task.Task, 0)
	for rows.Next() {
		var dbt dbTask
		if err := rows.Scan(
			&dbt.ID, &dbt.Name, &dbt.State, &dbt.ImageURL,
			&dbt.File, &dbt.CLIArgs, &dbt.Inputs, &dbt.Env,
			&dbt.Daemon, &dbt.Encrypted, &dbt.KBSResourcePath, &dbt.PropletID,
			&dbt.Results, &dbt.Error, &dbt.MonitoringProfile,
			&dbt.StartTime, &dbt.FinishTime, &dbt.CreatedAt, &dbt.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrDBScan, err)
		}

		t, err := r.toTask(dbt)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrDBScan, err)
		}

		tasks = append(tasks, t)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return tasks, total, nil
}

func (r *taskRepo) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM tasks WHERE id = ?`

	if _, err := r.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("%w: %w", ErrDelete, err)
	}

	return nil
}

func (r *taskRepo) toTask(dbt dbTask) (task.Task, error) {
	t := task.Task{
		ID:        dbt.ID,
		Name:      dbt.Name,
		State:     task.State(dbt.State),
		File:      dbt.File,
		Daemon:    dbt.Daemon,
		Encrypted: dbt.Encrypted,
		CreatedAt: dbt.CreatedAt.Time,
		UpdatedAt: dbt.UpdatedAt.Time,
	}

	if dbt.ImageURL != nil {
		t.ImageURL = *dbt.ImageURL
	}
	if dbt.CLIArgs != nil {
		if err := jsonUnmarshal(dbt.CLIArgs, &t.CLIArgs); err != nil {
			return task.Task{}, err
		}
	}
	if dbt.Inputs != nil {
		if err := jsonUnmarshal(dbt.Inputs, &t.Inputs); err != nil {
			return task.Task{}, err
		}
	}
	if dbt.Env != nil {
		if err := jsonUnmarshal(dbt.Env, &t.Env); err != nil {
			return task.Task{}, err
		}
	}
	if dbt.KBSResourcePath != nil {
		t.KBSResourcePath = *dbt.KBSResourcePath
	}
	if dbt.PropletID != nil {
		t.PropletID = *dbt.PropletID
	}
	if dbt.Results != nil {
		if err := jsonUnmarshal(dbt.Results, &t.Results); err != nil {
			return task.Task{}, err
		}
	}
	if dbt.Error != nil {
		t.Error = *dbt.Error
	}
	if dbt.MonitoringProfile != nil {
		if err := jsonUnmarshal(dbt.MonitoringProfile, &t.MonitoringProfile); err != nil {
			return task.Task{}, err
		}
	}
	if dbt.StartTime.Valid {
		t.StartTime = dbt.StartTime.Time
	}
	if dbt.FinishTime.Valid {
		t.FinishTime = dbt.FinishTime.Time
	}

	return t, nil
}

func jsonBytes(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}

	return json.Marshal(v)
}

func jsonUnmarshal(data []byte, v any) error {
	if data == nil {
		return nil
	}

	return json.Unmarshal(data, v)
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return &t
}
