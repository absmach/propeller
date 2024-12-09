package manager

import (
	"context"
	"time"

	"github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/proplet"
	"github.com/absmach/propeller/task"
	"github.com/google/uuid"
)

type service struct {
	tasksDB        storage.Storage
	propletsDB     storage.Storage
	propletsTaskDB storage.Storage
	taskPropletDB  storage.Storage
	scheduler      scheduler.Scheduler
}

func NewService(
	tasksDB, propletsDB, propletsTaskDB, taskPropletDB storage.Storage,
	s scheduler.Scheduler,
) Service {
	return &service{
		tasksDB:        tasksDB,
		propletsDB:     propletsDB,
		propletsTaskDB: propletsTaskDB,
		taskPropletDB:  taskPropletDB,
		scheduler:      s,
	}
}

func (svc *service) CreateProplet(ctx context.Context, w proplet.Proplet) (proplet.Proplet, error) {
	w.ID = uuid.New().String()
	if err := svc.propletsDB.Create(ctx, w.ID, w); err != nil {
		return proplet.Proplet{}, err
	}

	return w, nil
}

func (svc *service) GetProplet(ctx context.Context, propletID string) (proplet.Proplet, error) {
	data, err := svc.propletsDB.Get(ctx, propletID)
	if err != nil {
		return proplet.Proplet{}, err
	}
	w, ok := data.(proplet.Proplet)
	if !ok {
		return proplet.Proplet{}, errors.ErrInvalidData
	}

	return w, nil
}

func (svc *service) ListProplets(ctx context.Context, offset, limit uint64) (proplet.PropletPage, error) {
	data, total, err := svc.propletsDB.List(ctx, offset, limit)
	if err != nil {
		return proplet.PropletPage{}, err
	}
	proplets := make([]proplet.Proplet, total)
	for i := range data {
		w, ok := data[i].(proplet.Proplet)
		if !ok {
			return proplet.PropletPage{}, errors.ErrInvalidData
		}
		proplets[i] = w
	}

	return proplet.PropletPage{
		Offset:   offset,
		Limit:    limit,
		Total:    total,
		Proplets: proplets,
	}, nil
}

func (svc *service) UpdateProplet(ctx context.Context, w proplet.Proplet) (proplet.Proplet, error) {
	if err := svc.propletsDB.Update(ctx, w.ID, w); err != nil {
		return proplet.Proplet{}, err
	}

	return w, nil
}

func (svc *service) DeleteProplet(ctx context.Context, propletID string) error {
	return svc.propletsDB.Delete(ctx, propletID)
}

func (svc *service) SelectProplet(_ context.Context, t task.Task) (proplet.Proplet, error) {
	return svc.scheduler.SelectProplet(t, nil)
}

func (svc *service) CreateTask(ctx context.Context, t task.Task) (task.Task, error) {
	t.ID = uuid.NewString()
	t.CreatedAt = time.Now()

	if err := svc.tasksDB.Create(ctx, t.ID, t); err != nil {
		return task.Task{}, err
	}

	return t, nil
}

func (svc *service) GetTask(ctx context.Context, taskID string) (task.Task, error) {
	data, err := svc.tasksDB.Get(ctx, taskID)
	if err != nil {
		return task.Task{}, err
	}
	t, ok := data.(task.Task)
	if !ok {
		return task.Task{}, errors.ErrInvalidData
	}

	return t, nil
}

func (svc *service) ListTasks(ctx context.Context, offset, limit uint64) (task.TaskPage, error) {
	data, total, err := svc.tasksDB.List(ctx, offset, limit)
	if err != nil {
		return task.TaskPage{}, err
	}

	tasks := make([]task.Task, total)
	for i := range data {
		t, ok := data[i].(task.Task)
		if !ok {
			return task.TaskPage{}, errors.ErrInvalidData
		}

		tasks[i] = t
	}

	return task.TaskPage{
		Offset: offset,
		Limit:  limit,
		Total:  total,
		Tasks:  tasks,
	}, nil
}

func (svc *service) UpdateTask(ctx context.Context, t task.Task) (task.Task, error) {
	if err := svc.tasksDB.Update(ctx, t.ID, t); err != nil {
		return task.Task{}, err
	}

	return t, nil
}

func (svc *service) DeleteTask(ctx context.Context, taskID string) error {
	return svc.tasksDB.Delete(ctx, taskID)
}
