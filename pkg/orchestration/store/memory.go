package store

import (
	"context"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/orchestration"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/task"
)

type MemoryStateStore struct {
	tasksDB       storage.Storage
	propletsDB    storage.Storage
	taskPropletDB storage.Storage
	roundsDB      storage.Storage
}

func NewMemoryStateStore(
	tasksDB, propletsDB, taskPropletDB, roundsDB storage.Storage,
) orchestration.StateStore {
	return &MemoryStateStore{
		tasksDB:       tasksDB,
		propletsDB:    propletsDB,
		taskPropletDB: taskPropletDB,
		roundsDB:      roundsDB,
	}
}

func (s *MemoryStateStore) CreateTask(ctx context.Context, t orchestration.Task) error {
	return s.tasksDB.Create(ctx, t.ID, t)
}

func (s *MemoryStateStore) GetTask(ctx context.Context, taskID string) (orchestration.Task, error) {
	data, err := s.tasksDB.Get(ctx, taskID)
	if err != nil {
		return orchestration.Task{}, err
	}

	t, ok := data.(task.Task)
	if !ok {
		return orchestration.Task{}, pkgerrors.ErrInvalidData
	}

	return t, nil
}

func (s *MemoryStateStore) UpdateTask(ctx context.Context, t orchestration.Task) error {
	return s.tasksDB.Update(ctx, t.ID, t)
}

func (s *MemoryStateStore) DeleteTask(ctx context.Context, taskID string) error {
	return s.tasksDB.Delete(ctx, taskID)
}

func (s *MemoryStateStore) ListTasks(ctx context.Context, offset, limit uint64) (tasks []orchestration.Task, total uint64, err error) {
	data, total, err := s.tasksDB.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}

	tasks = make([]orchestration.Task, 0, len(data))
	for i := range data {
		t, ok := data[i].(task.Task)
		if !ok {
			continue
		}
		tasks = append(tasks, t)
	}

	return tasks, total, nil
}

func (s *MemoryStateStore) CreateProplet(ctx context.Context, p orchestration.Proplet) error {
	return s.propletsDB.Create(ctx, p.ID, p)
}

func (s *MemoryStateStore) GetProplet(ctx context.Context, propletID string) (orchestration.Proplet, error) {
	data, err := s.propletsDB.Get(ctx, propletID)
	if err != nil {
		return orchestration.Proplet{}, err
	}

	p, ok := data.(proplet.Proplet)
	if !ok {
		return orchestration.Proplet{}, pkgerrors.ErrInvalidData
	}

	return p, nil
}

func (s *MemoryStateStore) UpdateProplet(ctx context.Context, p orchestration.Proplet) error {
	return s.propletsDB.Update(ctx, p.ID, p)
}

func (s *MemoryStateStore) ListProplets(ctx context.Context, offset, limit uint64) (proplets []orchestration.Proplet, total uint64, err error) {
	data, total, err := s.propletsDB.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}

	proplets = make([]orchestration.Proplet, 0, len(data))
	for i := range data {
		p, ok := data[i].(proplet.Proplet)
		if !ok {
			continue
		}
		proplets = append(proplets, p)
	}

	return proplets, total, nil
}

func (s *MemoryStateStore) PinTaskToProplet(ctx context.Context, taskID, propletID string) error {
	return s.taskPropletDB.Create(ctx, taskID, propletID)
}

func (s *MemoryStateStore) GetPropletForTask(ctx context.Context, taskID string) (string, error) {
	data, err := s.taskPropletDB.Get(ctx, taskID)
	if err != nil {
		return "", err
	}

	propletID, ok := data.(string)
	if !ok {
		return "", pkgerrors.ErrInvalidData
	}

	return propletID, nil
}

func (s *MemoryStateStore) UnpinTaskFromProplet(ctx context.Context, taskID string) error {
	return s.taskPropletDB.Delete(ctx, taskID)
}

func (s *MemoryStateStore) CreateRound(ctx context.Context, round orchestration.Round) error {
	return s.roundsDB.Create(ctx, round.RoundID, round)
}

func (s *MemoryStateStore) GetRound(ctx context.Context, roundID string) (orchestration.Round, error) {
	data, err := s.roundsDB.Get(ctx, roundID)
	if err != nil {
		return orchestration.Round{}, err
	}

	r, ok := data.(orchestration.Round)
	if !ok {
		return orchestration.Round{}, pkgerrors.ErrInvalidData
	}

	return r, nil
}

func (s *MemoryStateStore) UpdateRound(ctx context.Context, round orchestration.Round) error {
	return s.roundsDB.Update(ctx, round.RoundID, round)
}

func (s *MemoryStateStore) ListRounds(ctx context.Context, federatedJobID string) ([]orchestration.Round, error) {
	// For in-memory store, we need to list all and filter
	// In a real implementation, this would be optimized
	data, _, err := s.roundsDB.List(ctx, 0, 10000)
	if err != nil {
		return nil, err
	}

	rounds := make([]orchestration.Round, 0)
	for i := range data {
		r, ok := data[i].(orchestration.Round)
		if !ok {
			continue
		}
		if r.FederatedJobID == federatedJobID {
			rounds = append(rounds, r)
		}
	}

	return rounds, nil
}
