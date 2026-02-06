package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	migrate "github.com/rubenv/sql-migrate"
)

var (
	ErrDBConnection    = errors.New("database connection error")
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
	Delete(ctx context.Context, id string) error
}

type PropletRepository interface {
	Create(ctx context.Context, p proplet.Proplet) error
	Get(ctx context.Context, id string) (proplet.Proplet, error)
	Update(ctx context.Context, p proplet.Proplet) error
	List(ctx context.Context, offset, limit uint64) ([]proplet.Proplet, uint64, error)
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
	Metrics      MetricsRepository
}

func NewRepositories(db *Database) *Repositories {
	return &Repositories{
		Tasks:        NewTaskRepository(db),
		Proplets:     NewPropletRepository(db),
		TaskProplets: NewTaskPropletRepository(db),
		Metrics:      NewMetricsRepository(db),
	}
}

type Database struct {
	*sqlx.DB
}

func NewDatabase(host, port, user, pass, name, sslMode string) (*Database, error) {
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s", host, port, user, pass, name, sslMode)
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("database connection error: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	database := &Database{DB: db}

	if err := database.Migrate(); err != nil {
		return nil, err
	}

	return database, nil
}

func (db *Database) Migrate() error {
	migrations := &migrate.MemoryMigrationSource{
		Migrations: []*migrate.Migration{
			{
				Id: "1_create_tables",
				Up: []string{
					`CREATE TABLE IF NOT EXISTS tasks (
						id VARCHAR(36) PRIMARY KEY,
						name VARCHAR(255) NOT NULL,
						state SMALLINT NOT NULL DEFAULT 0,
						image_url TEXT,
						file BYTEA,
						cli_args JSONB,
						inputs JSONB,
						env JSONB,
						daemon BOOLEAN DEFAULT FALSE,
						encrypted BOOLEAN DEFAULT FALSE,
						kbs_resource_path TEXT,
						proplet_id VARCHAR(36),
						results JSONB,
						error TEXT,
						monitoring_profile JSONB,
						start_time TIMESTAMPTZ,
						finish_time TIMESTAMPTZ,
						created_at TIMESTAMPTZ NOT NULL,
						updated_at TIMESTAMPTZ NOT NULL
					)`,
					`CREATE INDEX IF NOT EXISTS idx_tasks_state ON tasks(state)`,
					`CREATE INDEX IF NOT EXISTS idx_tasks_proplet_id ON tasks(proplet_id)`,
					`CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at DESC)`,
					`CREATE TABLE IF NOT EXISTS proplets (
						id VARCHAR(36) PRIMARY KEY,
						name VARCHAR(255) NOT NULL,
						task_count BIGINT DEFAULT 0,
						alive BOOLEAN DEFAULT FALSE,
						alive_history JSONB
					)`,
					`CREATE INDEX IF NOT EXISTS idx_proplets_alive ON proplets(alive)`,
					`CREATE TABLE IF NOT EXISTS task_proplets (
						task_id VARCHAR(36) PRIMARY KEY,
						proplet_id VARCHAR(36) NOT NULL,
						created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
						FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
						FOREIGN KEY (proplet_id) REFERENCES proplets(id) ON DELETE CASCADE
					)`,
					`CREATE INDEX IF NOT EXISTS idx_task_proplets_proplet_id ON task_proplets(proplet_id)`,
					`CREATE TABLE IF NOT EXISTS task_metrics (
						id VARCHAR(72) PRIMARY KEY,
						task_id VARCHAR(36) NOT NULL,
						proplet_id VARCHAR(36) NOT NULL,
						metrics JSONB NOT NULL,
						aggregated JSONB,
						timestamp TIMESTAMPTZ NOT NULL,
						FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
					)`,
					`CREATE INDEX IF NOT EXISTS idx_task_metrics_task_id ON task_metrics(task_id, timestamp DESC)`,
					`CREATE INDEX IF NOT EXISTS idx_task_metrics_timestamp ON task_metrics(timestamp DESC)`,
					`CREATE TABLE IF NOT EXISTS proplet_metrics (
						id VARCHAR(72) PRIMARY KEY,
						proplet_id VARCHAR(36) NOT NULL,
						namespace VARCHAR(255),
						cpu_metrics JSONB NOT NULL,
						memory_metrics JSONB NOT NULL,
						timestamp TIMESTAMPTZ NOT NULL,
						FOREIGN KEY (proplet_id) REFERENCES proplets(id) ON DELETE CASCADE
					)`,
					`CREATE INDEX IF NOT EXISTS idx_proplet_metrics_proplet_id ON proplet_metrics(proplet_id, timestamp DESC)`,
					`CREATE INDEX IF NOT EXISTS idx_proplet_metrics_timestamp ON proplet_metrics(timestamp DESC)`,
				},
				Down: []string{
					`DROP INDEX IF EXISTS idx_proplet_metrics_timestamp`,
					`DROP INDEX IF EXISTS idx_proplet_metrics_proplet_id`,
					`DROP TABLE IF EXISTS proplet_metrics`,
					`DROP INDEX IF EXISTS idx_task_metrics_timestamp`,
					`DROP INDEX IF EXISTS idx_task_metrics_task_id`,
					`DROP TABLE IF EXISTS task_metrics`,
					`DROP INDEX IF EXISTS idx_task_proplets_proplet_id`,
					`DROP TABLE IF EXISTS task_proplets`,
					`DROP INDEX IF EXISTS idx_proplets_alive`,
					`DROP TABLE IF EXISTS proplets`,
					`DROP INDEX IF EXISTS idx_tasks_created_at`,
					`DROP INDEX IF EXISTS idx_tasks_proplet_id`,
					`DROP INDEX IF EXISTS idx_tasks_state`,
					`DROP TABLE IF EXISTS tasks`,
				},
			},
		},
	}

	n, err := migrate.Exec(db.DB.DB, "postgres", migrations, migrate.Up)
	if err != nil {
		return fmt.Errorf("database migration error: %w", err)
	}

	if n > 0 {
		return fmt.Errorf("applied %d migrations", n)
	}

	return nil
}
