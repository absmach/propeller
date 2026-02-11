package storage

import (
	"context"
	"fmt"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/storage/badger"
	"github.com/absmach/propeller/pkg/storage/postgres"
	"github.com/absmach/propeller/pkg/storage/sqlite"
	"github.com/absmach/propeller/task"
)

type Config struct {
	Type string `env:"MANAGER_STORAGE_TYPE" envDefault:"postgres"`

	PostgresHost    string `env:"MANAGER_POSTGRES_HOST"    envDefault:"localhost"`
	PostgresPort    string `env:"MANAGER_POSTGRES_PORT"    envDefault:"5432"`
	PostgresUser    string `env:"MANAGER_POSTGRES_USER"    envDefault:"propeller"`
	PostgresPass    string `env:"MANAGER_POSTGRES_PASS"    envDefault:"propeller"`
	PostgresDB      string `env:"MANAGER_POSTGRES_DB"      envDefault:"propeller"`
	PostgresSSLMode string `env:"MANAGER_POSTGRES_SSLMODE" envDefault:"disable"`

	SQLitePath string `env:"MANAGER_SQLITE_PATH" envDefault:"./propeller.db"`

	BadgerPath string `env:"MANAGER_BADGER_PATH" envDefault:"./data/badger"`
}

type Repositories struct {
	Tasks        TaskRepository
	Proplets     PropletRepository
	TaskProplets TaskPropletRepository
	Jobs         JobRepository
	Metrics      MetricsRepository
}

func NewRepositories(cfg Config) (*Repositories, error) {
	switch cfg.Type {
	case "postgres":
		return newPostgresRepositories(cfg)
	case "sqlite":
		return newSQLiteRepositories(cfg)
	case "badger":
		return newBadgerRepositories(cfg)
	case "memory":
		return newMemoryRepositories()
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}
}

func newPostgresRepositories(cfg Config) (*Repositories, error) {
	db, err := postgres.NewDatabase(
		cfg.PostgresHost,
		cfg.PostgresPort,
		cfg.PostgresUser,
		cfg.PostgresPass,
		cfg.PostgresDB,
		cfg.PostgresSSLMode,
	)
	if err != nil {
		return nil, err
	}

	repos := postgres.NewRepositories(db)

	return &Repositories{
		Tasks:        &postgresTaskAdapter{repo: repos.Tasks},
		Proplets:     &postgresPropletAdapter{repo: repos.Proplets},
		TaskProplets: &postgresTaskPropletAdapter{repo: repos.TaskProplets},
		Jobs:         &postgresJobAdapter{repo: repos.Jobs},
		Metrics:      &postgresMetricsAdapter{repo: repos.Metrics},
	}, nil
}

func newSQLiteRepositories(cfg Config) (*Repositories, error) {
	db, err := sqlite.NewDatabase(cfg.SQLitePath)
	if err != nil {
		return nil, err
	}

	repos := sqlite.NewRepositories(db)

	return &Repositories{
		Tasks:        &sqliteTaskAdapter{repo: repos.Tasks},
		Proplets:     &sqlitePropletAdapter{repo: repos.Proplets},
		TaskProplets: &sqliteTaskPropletAdapter{repo: repos.TaskProplets},
		Jobs:         &sqliteJobAdapter{repo: repos.Jobs},
		Metrics:      &sqliteMetricsAdapter{repo: repos.Metrics},
	}, nil
}

func newBadgerRepositories(cfg Config) (*Repositories, error) {
	db, err := badger.NewDatabase(cfg.BadgerPath)
	if err != nil {
		return nil, err
	}

	repos := badger.NewRepositories(db)

	return &Repositories{
		Tasks:        &badgerTaskAdapter{repo: repos.Tasks},
		Proplets:     &badgerPropletAdapter{repo: repos.Proplets},
		TaskProplets: &badgerTaskPropletAdapter{repo: repos.TaskProplets},
		Jobs:         &badgerJobAdapter{repo: repos.Jobs},
		Metrics:      &badgerMetricsAdapter{repo: repos.Metrics},
	}, nil
}

func newMemoryRepositories() (*Repositories, error) {
	taskStorage := NewInMemoryStorage()
	propletStorage := NewInMemoryStorage()
	taskPropletStorage := NewInMemoryStorage()
	jobStorage := NewInMemoryStorage()
	metricsStorage := NewInMemoryStorage()

	return &Repositories{
		Tasks:        newMemoryTaskRepository(taskStorage),
		Proplets:     newMemoryPropletRepository(propletStorage),
		TaskProplets: newMemoryTaskPropletRepository(taskPropletStorage),
		Jobs:         newMemoryJobRepository(jobStorage),
		Metrics:      newMemoryMetricsRepository(metricsStorage),
	}, nil
}

type postgresTaskAdapter struct {
	repo postgres.TaskRepository
}

func (a *postgresTaskAdapter) Create(ctx context.Context, t task.Task) (task.Task, error) {
	return a.repo.Create(ctx, t)
}

func (a *postgresTaskAdapter) Get(ctx context.Context, id string) (task.Task, error) {
	return a.repo.Get(ctx, id)
}

func (a *postgresTaskAdapter) Update(ctx context.Context, t task.Task) error {
	return a.repo.Update(ctx, t)
}

func (a *postgresTaskAdapter) List(ctx context.Context, offset, limit uint64) ([]task.Task, uint64, error) {
	return a.repo.List(ctx, offset, limit)
}

func (a *postgresTaskAdapter) ListByWorkflowID(ctx context.Context, workflowID string) ([]task.Task, error) {
	return a.repo.ListByWorkflowID(ctx, workflowID)
}

func (a *postgresTaskAdapter) ListByJobID(ctx context.Context, jobID string) ([]task.Task, error) {
	return a.repo.ListByJobID(ctx, jobID)
}

func (a *postgresTaskAdapter) Delete(ctx context.Context, id string) error {
	return a.repo.Delete(ctx, id)
}

type postgresPropletAdapter struct {
	repo postgres.PropletRepository
}

func (a *postgresPropletAdapter) Create(ctx context.Context, p proplet.Proplet) error {
	return a.repo.Create(ctx, p)
}

func (a *postgresPropletAdapter) Get(ctx context.Context, id string) (proplet.Proplet, error) {
	return a.repo.Get(ctx, id)
}

func (a *postgresPropletAdapter) Update(ctx context.Context, p proplet.Proplet) error {
	return a.repo.Update(ctx, p)
}

func (a *postgresPropletAdapter) List(ctx context.Context, offset, limit uint64) ([]proplet.Proplet, uint64, error) {
	return a.repo.List(ctx, offset, limit)
}

func (a *postgresPropletAdapter) Delete(ctx context.Context, id string) error {
	return a.repo.Delete(ctx, id)
}

type postgresTaskPropletAdapter struct {
	repo postgres.TaskPropletRepository
}

func (a *postgresTaskPropletAdapter) Create(ctx context.Context, taskID, propletID string) error {
	return a.repo.Create(ctx, taskID, propletID)
}

func (a *postgresTaskPropletAdapter) Get(ctx context.Context, taskID string) (string, error) {
	return a.repo.Get(ctx, taskID)
}

func (a *postgresTaskPropletAdapter) Delete(ctx context.Context, taskID string) error {
	return a.repo.Delete(ctx, taskID)
}

type postgresMetricsAdapter struct {
	repo postgres.MetricsRepository
}

func (a *postgresMetricsAdapter) CreateTaskMetrics(ctx context.Context, m TaskMetrics) error {
	pm := postgres.TaskMetrics{
		TaskID:     m.TaskID,
		PropletID:  m.PropletID,
		Metrics:    m.Metrics,
		Aggregated: m.Aggregated,
		Timestamp:  m.Timestamp,
	}

	return a.repo.CreateTaskMetrics(ctx, pm)
}

func (a *postgresMetricsAdapter) CreatePropletMetrics(ctx context.Context, m PropletMetrics) error {
	pm := postgres.PropletMetrics{
		PropletID: m.PropletID,
		Namespace: m.Namespace,
		Timestamp: m.Timestamp,
		CPU:       m.CPU,
		Memory:    m.Memory,
	}

	return a.repo.CreatePropletMetrics(ctx, pm)
}

func (a *postgresMetricsAdapter) ListTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) ([]TaskMetrics, uint64, error) {
	metrics, total, err := a.repo.ListTaskMetrics(ctx, taskID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	result := make([]TaskMetrics, len(metrics))
	for i, m := range metrics {
		result[i] = TaskMetrics{
			TaskID:     m.TaskID,
			PropletID:  m.PropletID,
			Metrics:    m.Metrics,
			Aggregated: m.Aggregated,
			Timestamp:  m.Timestamp,
		}
	}

	return result, total, nil
}

func (a *postgresMetricsAdapter) ListPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) ([]PropletMetrics, uint64, error) {
	metrics, total, err := a.repo.ListPropletMetrics(ctx, propletID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	result := make([]PropletMetrics, len(metrics))
	for i := range metrics {
		result[i] = PropletMetrics{
			PropletID: metrics[i].PropletID,
			Namespace: metrics[i].Namespace,
			Timestamp: metrics[i].Timestamp,
			CPU:       metrics[i].CPU,
			Memory:    metrics[i].Memory,
		}
	}

	return result, total, nil
}

type sqliteTaskAdapter struct {
	repo sqlite.TaskRepository
}

func (a *sqliteTaskAdapter) Create(ctx context.Context, t task.Task) (task.Task, error) {
	return a.repo.Create(ctx, t)
}

func (a *sqliteTaskAdapter) Get(ctx context.Context, id string) (task.Task, error) {
	return a.repo.Get(ctx, id)
}

func (a *sqliteTaskAdapter) Update(ctx context.Context, t task.Task) error {
	return a.repo.Update(ctx, t)
}

func (a *sqliteTaskAdapter) List(ctx context.Context, offset, limit uint64) ([]task.Task, uint64, error) {
	return a.repo.List(ctx, offset, limit)
}

func (a *sqliteTaskAdapter) ListByWorkflowID(ctx context.Context, workflowID string) ([]task.Task, error) {
	return a.repo.ListByWorkflowID(ctx, workflowID)
}

func (a *sqliteTaskAdapter) ListByJobID(ctx context.Context, jobID string) ([]task.Task, error) {
	return a.repo.ListByJobID(ctx, jobID)
}

func (a *sqliteTaskAdapter) Delete(ctx context.Context, id string) error {
	return a.repo.Delete(ctx, id)
}

type sqlitePropletAdapter struct {
	repo sqlite.PropletRepository
}

func (a *sqlitePropletAdapter) Create(ctx context.Context, p proplet.Proplet) error {
	return a.repo.Create(ctx, p)
}

func (a *sqlitePropletAdapter) Get(ctx context.Context, id string) (proplet.Proplet, error) {
	return a.repo.Get(ctx, id)
}

func (a *sqlitePropletAdapter) Update(ctx context.Context, p proplet.Proplet) error {
	return a.repo.Update(ctx, p)
}

func (a *sqlitePropletAdapter) List(ctx context.Context, offset, limit uint64) ([]proplet.Proplet, uint64, error) {
	return a.repo.List(ctx, offset, limit)
}

func (a *sqlitePropletAdapter) Delete(ctx context.Context, id string) error {
	return a.repo.Delete(ctx, id)
}

type sqliteTaskPropletAdapter struct {
	repo sqlite.TaskPropletRepository
}

func (a *sqliteTaskPropletAdapter) Create(ctx context.Context, taskID, propletID string) error {
	return a.repo.Create(ctx, taskID, propletID)
}

func (a *sqliteTaskPropletAdapter) Get(ctx context.Context, taskID string) (string, error) {
	return a.repo.Get(ctx, taskID)
}

func (a *sqliteTaskPropletAdapter) Delete(ctx context.Context, taskID string) error {
	return a.repo.Delete(ctx, taskID)
}

type sqliteMetricsAdapter struct {
	repo sqlite.MetricsRepository
}

func (a *sqliteMetricsAdapter) CreateTaskMetrics(ctx context.Context, m TaskMetrics) error {
	sm := sqlite.TaskMetrics{
		TaskID:     m.TaskID,
		PropletID:  m.PropletID,
		Metrics:    m.Metrics,
		Aggregated: m.Aggregated,
		Timestamp:  m.Timestamp,
	}

	return a.repo.CreateTaskMetrics(ctx, sm)
}

func (a *sqliteMetricsAdapter) CreatePropletMetrics(ctx context.Context, m PropletMetrics) error {
	sm := sqlite.PropletMetrics{
		PropletID: m.PropletID,
		Namespace: m.Namespace,
		Timestamp: m.Timestamp,
		CPU:       m.CPU,
		Memory:    m.Memory,
	}

	return a.repo.CreatePropletMetrics(ctx, sm)
}

func (a *sqliteMetricsAdapter) ListTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) ([]TaskMetrics, uint64, error) {
	metrics, total, err := a.repo.ListTaskMetrics(ctx, taskID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	result := make([]TaskMetrics, len(metrics))
	for i, m := range metrics {
		result[i] = TaskMetrics{
			TaskID:     m.TaskID,
			PropletID:  m.PropletID,
			Metrics:    m.Metrics,
			Aggregated: m.Aggregated,
			Timestamp:  m.Timestamp,
		}
	}

	return result, total, nil
}

func (a *sqliteMetricsAdapter) ListPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) ([]PropletMetrics, uint64, error) {
	metrics, total, err := a.repo.ListPropletMetrics(ctx, propletID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	result := make([]PropletMetrics, len(metrics))
	for i := range metrics {
		result[i] = PropletMetrics{
			PropletID: metrics[i].PropletID,
			Namespace: metrics[i].Namespace,
			Timestamp: metrics[i].Timestamp,
			CPU:       metrics[i].CPU,
			Memory:    metrics[i].Memory,
		}
	}

	return result, total, nil
}

type badgerTaskAdapter struct {
	repo badger.TaskRepository
}

func (a *badgerTaskAdapter) Create(ctx context.Context, t task.Task) (task.Task, error) {
	return a.repo.Create(ctx, t)
}

func (a *badgerTaskAdapter) Get(ctx context.Context, id string) (task.Task, error) {
	return a.repo.Get(ctx, id)
}

func (a *badgerTaskAdapter) Update(ctx context.Context, t task.Task) error {
	return a.repo.Update(ctx, t)
}

func (a *badgerTaskAdapter) List(ctx context.Context, offset, limit uint64) ([]task.Task, uint64, error) {
	return a.repo.List(ctx, offset, limit)
}

func (a *badgerTaskAdapter) ListByWorkflowID(ctx context.Context, workflowID string) ([]task.Task, error) {
	return a.repo.ListByWorkflowID(ctx, workflowID)
}

func (a *badgerTaskAdapter) ListByJobID(ctx context.Context, jobID string) ([]task.Task, error) {
	return a.repo.ListByJobID(ctx, jobID)
}

func (a *badgerTaskAdapter) Delete(ctx context.Context, id string) error {
	return a.repo.Delete(ctx, id)
}

type badgerPropletAdapter struct {
	repo badger.PropletRepository
}

func (a *badgerPropletAdapter) Create(ctx context.Context, p proplet.Proplet) error {
	return a.repo.Create(ctx, p)
}

func (a *badgerPropletAdapter) Get(ctx context.Context, id string) (proplet.Proplet, error) {
	return a.repo.Get(ctx, id)
}

func (a *badgerPropletAdapter) Update(ctx context.Context, p proplet.Proplet) error {
	return a.repo.Update(ctx, p)
}

func (a *badgerPropletAdapter) List(ctx context.Context, offset, limit uint64) ([]proplet.Proplet, uint64, error) {
	return a.repo.List(ctx, offset, limit)
}

func (a *badgerPropletAdapter) Delete(ctx context.Context, id string) error {
	return a.repo.Delete(ctx, id)
}

type badgerTaskPropletAdapter struct {
	repo badger.TaskPropletRepository
}

func (a *badgerTaskPropletAdapter) Create(ctx context.Context, taskID, propletID string) error {
	return a.repo.Create(ctx, taskID, propletID)
}

func (a *badgerTaskPropletAdapter) Get(ctx context.Context, taskID string) (string, error) {
	return a.repo.Get(ctx, taskID)
}

func (a *badgerTaskPropletAdapter) Delete(ctx context.Context, taskID string) error {
	return a.repo.Delete(ctx, taskID)
}

type badgerMetricsAdapter struct {
	repo badger.MetricsRepository
}

func (a *badgerMetricsAdapter) CreateTaskMetrics(ctx context.Context, m TaskMetrics) error {
	bm := badger.TaskMetrics{
		TaskID:     m.TaskID,
		PropletID:  m.PropletID,
		Metrics:    m.Metrics,
		Aggregated: m.Aggregated,
		Timestamp:  m.Timestamp,
	}

	return a.repo.CreateTaskMetrics(ctx, bm)
}

func (a *badgerMetricsAdapter) CreatePropletMetrics(ctx context.Context, m PropletMetrics) error {
	bm := badger.PropletMetrics{
		PropletID: m.PropletID,
		Namespace: m.Namespace,
		Timestamp: m.Timestamp,
		CPU:       m.CPU,
		Memory:    m.Memory,
	}

	return a.repo.CreatePropletMetrics(ctx, bm)
}

func (a *badgerMetricsAdapter) ListTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) ([]TaskMetrics, uint64, error) {
	metrics, total, err := a.repo.ListTaskMetrics(ctx, taskID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	result := make([]TaskMetrics, len(metrics))
	for i, m := range metrics {
		result[i] = TaskMetrics{
			TaskID:     m.TaskID,
			PropletID:  m.PropletID,
			Metrics:    m.Metrics,
			Aggregated: m.Aggregated,
			Timestamp:  m.Timestamp,
		}
	}

	return result, total, nil
}

func (a *badgerMetricsAdapter) ListPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) ([]PropletMetrics, uint64, error) {
	metrics, total, err := a.repo.ListPropletMetrics(ctx, propletID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	result := make([]PropletMetrics, len(metrics))
	for i := range metrics {
		result[i] = PropletMetrics{
			PropletID: metrics[i].PropletID,
			Namespace: metrics[i].Namespace,
			Timestamp: metrics[i].Timestamp,
			CPU:       metrics[i].CPU,
			Memory:    metrics[i].Memory,
		}
	}

	return result, total, nil
}

type postgresJobAdapter struct {
	repo postgres.JobRepository
}

func (a *postgresJobAdapter) Create(ctx context.Context, j Job) (Job, error) {
	pj := postgres.Job{
		ID: j.ID, Name: j.Name, ExecutionMode: j.ExecutionMode,
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
	pj, err := a.repo.Create(ctx, pj)
	if err != nil {
		return Job{}, err
	}

	return Job{ID: pj.ID, Name: pj.Name, ExecutionMode: pj.ExecutionMode, CreatedAt: pj.CreatedAt, UpdatedAt: pj.UpdatedAt}, nil
}

func (a *postgresJobAdapter) Get(ctx context.Context, id string) (Job, error) {
	pj, err := a.repo.Get(ctx, id)
	if err != nil {
		return Job{}, err
	}

	return Job{ID: pj.ID, Name: pj.Name, ExecutionMode: pj.ExecutionMode, CreatedAt: pj.CreatedAt, UpdatedAt: pj.UpdatedAt}, nil
}

func (a *postgresJobAdapter) List(ctx context.Context, offset, limit uint64) ([]Job, uint64, error) {
	pjobs, total, err := a.repo.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	jobs := make([]Job, len(pjobs))
	for i, pj := range pjobs {
		jobs[i] = Job{ID: pj.ID, Name: pj.Name, ExecutionMode: pj.ExecutionMode, CreatedAt: pj.CreatedAt, UpdatedAt: pj.UpdatedAt}
	}

	return jobs, total, nil
}

func (a *postgresJobAdapter) Delete(ctx context.Context, id string) error {
	return a.repo.Delete(ctx, id)
}

type sqliteJobAdapter struct {
	repo sqlite.JobRepository
}

func (a *sqliteJobAdapter) Create(ctx context.Context, j Job) (Job, error) {
	sj := sqlite.Job{
		ID: j.ID, Name: j.Name, ExecutionMode: j.ExecutionMode,
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
	sj, err := a.repo.Create(ctx, sj)
	if err != nil {
		return Job{}, err
	}

	return Job{ID: sj.ID, Name: sj.Name, ExecutionMode: sj.ExecutionMode, CreatedAt: sj.CreatedAt, UpdatedAt: sj.UpdatedAt}, nil
}

func (a *sqliteJobAdapter) Get(ctx context.Context, id string) (Job, error) {
	sj, err := a.repo.Get(ctx, id)
	if err != nil {
		return Job{}, err
	}

	return Job{ID: sj.ID, Name: sj.Name, ExecutionMode: sj.ExecutionMode, CreatedAt: sj.CreatedAt, UpdatedAt: sj.UpdatedAt}, nil
}

func (a *sqliteJobAdapter) List(ctx context.Context, offset, limit uint64) ([]Job, uint64, error) {
	sjobs, total, err := a.repo.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	jobs := make([]Job, len(sjobs))
	for i, sj := range sjobs {
		jobs[i] = Job{ID: sj.ID, Name: sj.Name, ExecutionMode: sj.ExecutionMode, CreatedAt: sj.CreatedAt, UpdatedAt: sj.UpdatedAt}
	}

	return jobs, total, nil
}

func (a *sqliteJobAdapter) Delete(ctx context.Context, id string) error {
	return a.repo.Delete(ctx, id)
}

type badgerJobAdapter struct {
	repo badger.JobRepository
}

func (a *badgerJobAdapter) Create(ctx context.Context, j Job) (Job, error) {
	bj := badger.Job{
		ID: j.ID, Name: j.Name, ExecutionMode: j.ExecutionMode,
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
	bj, err := a.repo.Create(ctx, bj)
	if err != nil {
		return Job{}, err
	}

	return Job{ID: bj.ID, Name: bj.Name, ExecutionMode: bj.ExecutionMode, CreatedAt: bj.CreatedAt, UpdatedAt: bj.UpdatedAt}, nil
}

func (a *badgerJobAdapter) Get(ctx context.Context, id string) (Job, error) {
	bj, err := a.repo.Get(ctx, id)
	if err != nil {
		return Job{}, err
	}

	return Job{ID: bj.ID, Name: bj.Name, ExecutionMode: bj.ExecutionMode, CreatedAt: bj.CreatedAt, UpdatedAt: bj.UpdatedAt}, nil
}

func (a *badgerJobAdapter) List(ctx context.Context, offset, limit uint64) ([]Job, uint64, error) {
	bjobs, total, err := a.repo.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	jobs := make([]Job, len(bjobs))
	for i, bj := range bjobs {
		jobs[i] = Job{ID: bj.ID, Name: bj.Name, ExecutionMode: bj.ExecutionMode, CreatedAt: bj.CreatedAt, UpdatedAt: bj.UpdatedAt}
	}

	return jobs, total, nil
}

func (a *badgerJobAdapter) Delete(ctx context.Context, id string) error {
	return a.repo.Delete(ctx, id)
}
