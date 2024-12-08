package api

import (
	"context"
	"errors"

	"github.com/absmach/magistrala/pkg/apiutil"
	"github.com/absmach/propeller/manager"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/go-kit/kit/endpoint"
)

func createWorkerEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req, ok := request.(workerReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return workerResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		node, err := svc.CreateWorker(ctx, req.Worker)
		if err != nil {
			return workerResponse{}, err
		}

		return workerResponse{
			Worker: node,
		}, nil
	}
}

func listWorkersEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req, ok := request.(listEntityReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return listWorkerResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		nodes, err := svc.ListWorkers(ctx, req.offset, req.limit)
		if err != nil {
			return listWorkerResponse{}, err
		}

		return listWorkerResponse{
			WorkerPage: nodes,
		}, nil
	}
}

func getWorkerEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req, ok := request.(entityReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return workerResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		node, err := svc.GetWorker(ctx, req.id)
		if err != nil {
			return workerResponse{}, err
		}

		return workerResponse{
			Worker: node,
		}, nil
	}
}

func updateWorkerEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req, ok := request.(workerReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return workerResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		node, err := svc.UpdateWorker(ctx, req.Worker)
		if err != nil {
			return workerResponse{}, err
		}

		return workerResponse{
			Worker: node,
		}, nil
	}
}

func deleteWorkerEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req, ok := request.(entityReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return workerResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		if err := svc.DeleteWorker(ctx, req.id); err != nil {
			return workerResponse{}, err
		}

		return workerResponse{
			deleted: true,
		}, nil
	}
}

func createTaskEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req, ok := request.(taskReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		task, err := svc.CreateTask(ctx, req.Task)
		if err != nil {
			return taskResponse{}, err
		}

		return taskResponse{
			Task:    task,
			created: true,
		}, nil
	}
}

func listTasksEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req, ok := request.(listEntityReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return listTaskResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		tasks, err := svc.ListTasks(ctx, req.offset, req.limit)
		if err != nil {
			return listTaskResponse{}, err
		}

		return listTaskResponse{
			TaskPage: tasks,
		}, nil
	}
}

func getTaskEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req, ok := request.(entityReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		task, err := svc.GetTask(ctx, req.id)
		if err != nil {
			return taskResponse{}, err
		}

		return taskResponse{
			Task: task,
		}, nil
	}
}

func updateTaskEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req, ok := request.(taskReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		task, err := svc.UpdateTask(ctx, req.Task)
		if err != nil {
			return taskResponse{}, err
		}

		return taskResponse{
			Task: task,
		}, nil
	}
}

func deleteTaskEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req, ok := request.(entityReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		err := svc.DeleteTask(ctx, req.id)
		if err != nil {
			return taskResponse{}, err
		}

		return taskResponse{
			deleted: true,
		}, nil
	}
}
