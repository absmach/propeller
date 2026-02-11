package api

import (
	"context"
	"errors"

	"github.com/absmach/propeller/manager"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	apiutil "github.com/absmach/supermq/api/http/util"
	"github.com/go-kit/kit/endpoint"
)

func listPropletsEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(listEntityReq)
		if !ok {
			return listpropletResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return listpropletResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		proplets, err := svc.ListProplets(ctx, req.offset, req.limit)
		if err != nil {
			return listpropletResponse{}, err
		}

		return listpropletResponse{
			PropletPage: proplets,
		}, nil
	}
}

func getPropletEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(entityReq)
		if !ok {
			return propletResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return propletResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		node, err := svc.GetProplet(ctx, req.id)
		if err != nil {
			return propletResponse{}, err
		}

		return propletResponse{
			Proplet: node,
		}, nil
	}
}

func deletePropletEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(entityReq)
		if !ok {
			return propletResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return propletResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		if err := svc.DeleteProplet(ctx, req.id); err != nil {
			return propletResponse{}, err
		}

		return propletResponse{
			deleted: true,
		}, nil
	}
}

func createTaskEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
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
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(listEntityReq)
		if !ok {
			return listTaskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
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
	return func(ctx context.Context, request any) (any, error) {
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
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(taskReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
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
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(entityReq)
		if !ok {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return taskResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		if err := svc.DeleteTask(ctx, req.id); err != nil {
			return taskResponse{}, err
		}

		return taskResponse{
			deleted: true,
		}, nil
	}
}

func startTaskEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(entityReq)
		if !ok {
			return messageResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return messageResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		if err := svc.StartTask(ctx, req.id); err != nil {
			return messageResponse{}, err
		}

		return messageResponse{
			"started": true,
		}, nil
	}
}

func stopTaskEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(entityReq)
		if !ok {
			return messageResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return messageResponse{}, errors.Join(apiutil.ErrValidation, err)
		}
		if err := svc.StopTask(ctx, req.id); err != nil {
			return messageResponse{}, err
		}

		return messageResponse{
			"stopped": true,
		}, nil
	}
}

func getTaskMetricsEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(metricsReq)
		if !ok {
			return taskMetricsResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return taskMetricsResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		metrics, err := svc.GetTaskMetrics(ctx, req.id, req.offset, req.limit)
		if err != nil {
			return taskMetricsResponse{}, err
		}

		return taskMetricsResponse{
			TaskMetricsPage: metrics,
		}, nil
	}
}

func getPropletMetricsEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(metricsReq)
		if !ok {
			return propletMetricsResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}
		if err := req.validate(); err != nil {
			return propletMetricsResponse{}, errors.Join(apiutil.ErrValidation, err)
		}

		metrics, err := svc.GetPropletMetrics(ctx, req.id, req.offset, req.limit)
		if err != nil {
			return propletMetricsResponse{}, err
		}

		return propletMetricsResponse{
			PropletMetricsPage: metrics,
		}, nil
	}
}
