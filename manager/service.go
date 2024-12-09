package manager

import (
	"context"
	"time"

	"github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/proplet"
	"github.com/absmach/propeller/task"
	"github.com/google/uuid"
)

const (
	defOffset = 0
	defLimit  = 100
)

type service struct {
	tasksDB       storage.Storage
	propletsDB    storage.Storage
	taskPropletDB storage.Storage
	scheduler     scheduler.Scheduler
	publisher     mqtt.PubSub
}

func NewService(tasksDB, propletsDB, taskPropletDB storage.Storage, s scheduler.Scheduler, publisher mqtt.PubSub) Service {
	return &service{
		tasksDB:       tasksDB,
		propletsDB:    propletsDB,
		taskPropletDB: taskPropletDB,
		scheduler:     s,
		publisher:     publisher,
	}
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
	w.SetAlive()

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
		w.SetAlive()
		proplets[i] = w
	}

	return proplet.PropletPage{
		Offset:   offset,
		Limit:    limit,
		Total:    total,
		Proplets: proplets,
	}, nil
}

func (svc *service) SelectProplet(ctx context.Context, t task.Task) (proplet.Proplet, error) {
	proplets, err := svc.ListProplets(ctx, defOffset, defLimit)
	if err != nil {
		return proplet.Proplet{}, err
	}

	return svc.scheduler.SelectProplet(t, proplets.Proplets)
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

func (svc *service) StartTask(ctx context.Context, taskID string) error {
	t, err := svc.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	p, err := svc.SelectProplet(ctx, t)
	if err != nil {
		return err
	}

	topic := "channels/" + p.ID + "/messages/control/manager/start"
	if err := svc.publisher.Publish(ctx, topic, t); err != nil {
		return err
	}

	if err := svc.taskPropletDB.Create(ctx, taskID, p.ID); err != nil {
		return err
	}

	p.TaskCount++
	if err := svc.propletsDB.Update(ctx, p.ID, p); err != nil {
		return err
	}

	return nil
}

func (svc *service) StopTask(ctx context.Context, taskID string) error {
	t, err := svc.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	data, err := svc.taskPropletDB.Get(ctx, taskID)
	if err != nil {
		return err
	}
	propellerID, ok := data.(string)
	if !ok {
		return errors.ErrInvalidData
	}
	p, err := svc.GetProplet(ctx, propellerID)
	if err != nil {
		return err
	}

	topic := "channels/" + p.ID + "/messages/control/manager/stop"
	if err := svc.publisher.Publish(ctx, topic, t); err != nil {
		return err
	}

	if err := svc.taskPropletDB.Delete(ctx, taskID); err != nil {
		return err
	}

	p.TaskCount--
	if err := svc.propletsDB.Update(ctx, p.ID, p); err != nil {
		return err
	}

	return nil
}
