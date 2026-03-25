package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/absmach/propeller/pkg/task"
)

type taskRepo struct {
	db *Database
}

func NewTaskRepository(db *Database) TaskRepository {
	return &taskRepo{db: db}
}

type dbTask struct {
	ID                string        `db:"id"`
	Name              string        `db:"name"`
	State             uint8         `db:"state"`
	ImageURL          *string       `db:"image_url"`
	File              []byte        `db:"file"`
	CLIArgs           []byte        `db:"cli_args"`
	Inputs            []byte        `db:"inputs"`
	Env               []byte        `db:"env"`
	Daemon            bool          `db:"daemon"`
	Encrypted         bool          `db:"encrypted"`
	KBSResourcePath   *string       `db:"kbs_resource_path"`
	PropletID         *string       `db:"proplet_id"`
	Results           []byte        `db:"results"`
	Error             *string       `db:"error"`
	MonitoringProfile []byte        `db:"monitoring_profile"`
	StartTime         *sql.NullTime `db:"start_time"`
	FinishTime        *sql.NullTime `db:"finish_time"`
	CreatedAt         sql.NullTime  `db:"created_at"`
	UpdatedAt         sql.NullTime  `db:"updated_at"`
	WorkflowID        *string       `db:"workflow_id"`
	JobID             *string       `db:"job_id"`
	DependsOn         []byte        `db:"depends_on"`
	RunIf             *string       `db:"run_if"`
	Kind              *string       `db:"kind"`
	Mode              *string       `db:"mode"`
}

const taskColumns = `id, name, state, image_url, file, cli_args, inputs, env, daemon, encrypted,
	kbs_resource_path, proplet_id, results, error, monitoring_profile, start_time, finish_time,
	created_at, updated_at, workflow_id, job_id, depends_on, run_if, kind, mode`

func (r *taskRepo) Create(ctx context.Context, t task.Task) (task.Task, error) {
	query := `INSERT INTO tasks (` + taskColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25)`

	cliArgs, err := jsonBytes(t.CLIArgs)
	if err != nil {
		return task.Task{}, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	inputs, err := jsonBytes(t.Inputs)
	if err != nil {
		return task.Task{}, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	env, err := jsonBytes(t.Env)
	if err != nil {
		return task.Task{}, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	results, err := jsonBytes(t.Results)
	if err != nil {
		return task.Task{}, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	monitoringProfile, err := jsonBytes(t.MonitoringProfile)
	if err != nil {
		return task.Task{}, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	dependsOn, err := jsonBytes(t.DependsOn)
	if err != nil {
		return task.Task{}, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	_, err = r.db.ExecContext(ctx, query,
		t.ID, t.Name, uint8(t.State),
		nullString(t.ImageURL),
		t.File,
		cliArgs,
		inputs,
		env,
		t.Daemon, t.Encrypted,
		nullString(t.KBSResourcePath),
		nullString(t.PropletID),
		results,
		nullString(t.Error),
		monitoringProfile,
		nullTime(t.StartTime),
		nullTime(t.FinishTime),
		t.CreatedAt, t.UpdatedAt,
		nullString(t.WorkflowID),
		nullString(t.JobID),
		dependsOn,
		nullString(t.RunIf),
		nullString(string(t.Kind)),
		nullString(string(t.Mode)),
	)
	if err != nil {
		return task.Task{}, fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return t, nil
}

func (r *taskRepo) Get(ctx context.Context, id string) (task.Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE id = $1`

	var dbt dbTask

	if err := r.db.GetContext(ctx, &dbt, query, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return task.Task{}, ErrTaskNotFound
		}

		return task.Task{}, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return r.toTask(dbt)
}

func (r *taskRepo) Update(ctx context.Context, t task.Task) error {
	query := `UPDATE tasks SET
		name = $2, state = $3, image_url = $4, file = $5, cli_args = $6, inputs = $7,
		env = $8, daemon = $9, encrypted = $10, kbs_resource_path = $11, proplet_id = $12,
		results = $13, error = $14, monitoring_profile = $15, start_time = $16,
		finish_time = $17, updated_at = $18, workflow_id = $19, job_id = $20,
		depends_on = $21, run_if = $22, kind = $23, mode = $24
		WHERE id = $1`

	cliArgs, err := jsonBytes(t.CLIArgs)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	inputs, err := jsonBytes(t.Inputs)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	env, err := jsonBytes(t.Env)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	results, err := jsonBytes(t.Results)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	monitoringProfile, err := jsonBytes(t.MonitoringProfile)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	dependsOn, err := jsonBytes(t.DependsOn)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	_, err = r.db.ExecContext(ctx, query,
		t.ID, t.Name, uint8(t.State),
		nullString(t.ImageURL),
		t.File,
		cliArgs, inputs, env,
		t.Daemon, t.Encrypted,
		nullString(t.KBSResourcePath),
		nullString(t.PropletID),
		results,
		nullString(t.Error),
		monitoringProfile,
		nullTime(t.StartTime),
		nullTime(t.FinishTime),
		t.UpdatedAt,
		nullString(t.WorkflowID),
		nullString(t.JobID),
		dependsOn,
		nullString(t.RunIf),
		nullString(string(t.Kind)),
		nullString(string(t.Mode)),
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

	query := `SELECT ` + taskColumns + ` FROM tasks ORDER BY created_at DESC LIMIT $1 OFFSET $2`

	tasks, err := r.scanTasks(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

func (r *taskRepo) ListByWorkflowID(ctx context.Context, workflowID string) ([]task.Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE workflow_id = $1 ORDER BY created_at`

	return r.scanTasks(ctx, query, workflowID)
}

func (r *taskRepo) ListByJobID(ctx context.Context, jobID string) ([]task.Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE job_id = $1 ORDER BY created_at`

	return r.scanTasks(ctx, query, jobID)
}

func (r *taskRepo) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM tasks WHERE id = $1`

	if _, err := r.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("%w: %w", ErrDelete, err)
	}

	return nil
}

func (r *taskRepo) scanTasks(ctx context.Context, query string, args ...any) ([]task.Task, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDBQuery, err)
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
			&dbt.WorkflowID, &dbt.JobID, &dbt.DependsOn, &dbt.RunIf,
			&dbt.Kind, &dbt.Mode,
		); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDBScan, err)
		}

		t, err := r.toTask(dbt)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDBScan, err)
		}

		tasks = append(tasks, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDBQuery, err)
	}

	return tasks, nil
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
	if err := jsonUnmarshal(dbt.CLIArgs, &t.CLIArgs); err != nil {
		return task.Task{}, err
	}
	if err := jsonUnmarshal(dbt.Inputs, &t.Inputs); err != nil {
		return task.Task{}, err
	}
	if err := jsonUnmarshal(dbt.Env, &t.Env); err != nil {
		return task.Task{}, err
	}
	if dbt.KBSResourcePath != nil {
		t.KBSResourcePath = *dbt.KBSResourcePath
	}
	if dbt.PropletID != nil {
		t.PropletID = *dbt.PropletID
	}
	if err := jsonUnmarshal(dbt.Results, &t.Results); err != nil {
		return task.Task{}, err
	}
	if dbt.Error != nil {
		t.Error = *dbt.Error
	}
	if err := jsonUnmarshal(dbt.MonitoringProfile, &t.MonitoringProfile); err != nil {
		return task.Task{}, err
	}
	if dbt.StartTime != nil && dbt.StartTime.Valid {
		t.StartTime = dbt.StartTime.Time
	}
	if dbt.FinishTime != nil && dbt.FinishTime.Valid {
		t.FinishTime = dbt.FinishTime.Time
	}
	if dbt.WorkflowID != nil {
		t.WorkflowID = *dbt.WorkflowID
	}
	if dbt.JobID != nil {
		t.JobID = *dbt.JobID
	}
	if err := jsonUnmarshal(dbt.DependsOn, &t.DependsOn); err != nil {
		return task.Task{}, err
	}
	if dbt.RunIf != nil {
		t.RunIf = *dbt.RunIf
	}
	if dbt.Kind != nil {
		t.Kind = task.TaskKind(*dbt.Kind)
	}
	if dbt.Mode != nil {
		t.Mode = task.Mode(*dbt.Mode)
	}

	return t, nil
}
