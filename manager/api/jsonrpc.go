// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/api"
	"github.com/absmach/propeller/pkg/jsonrpc"
	"github.com/absmach/propeller/pkg/task"
)

func NewRPCServer(svc manager.Service) *jsonrpc.Server {
	srv := jsonrpc.NewServer()
	registerTaskMethods(srv, svc)
	registerPropletMethods(srv, svc)
	registerJobMethods(srv, svc)

	return srv
}

func decodeParams(raw json.RawMessage, v any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(trimmed, v); err != nil {
		return jsonrpc.InvalidParams("params could not be decoded")
	}

	return nil
}

func decodeID(raw json.RawMessage) (string, error) {
	var params jsonrpc.IDParams
	if err := decodeParams(raw, &params); err != nil {
		return "", err
	}
	if params.ID == "" {
		return "", jsonrpc.InvalidParams("id is required")
	}

	return params.ID, nil
}

func decodeList(raw json.RawMessage) (jsonrpc.ListParams, error) {
	var params jsonrpc.ListParams
	if err := decodeParams(raw, &params); err != nil {
		return params, err
	}
	if params.Limit == 0 {
		params.Limit = api.DefLimit
	}

	return params, nil
}

func decodeEntityList(raw json.RawMessage) (jsonrpc.EntityListParams, error) {
	var params jsonrpc.EntityListParams
	if err := decodeParams(raw, &params); err != nil {
		return params, err
	}
	if params.ID == "" {
		return params, jsonrpc.InvalidParams("id is required")
	}
	if params.Limit == 0 {
		params.Limit = api.DefLimit
	}

	return params, nil
}

func registerTaskMethods(srv *jsonrpc.Server, svc manager.Service) {
	srv.Register(jsonrpc.MethodTaskCreate, func(ctx context.Context, raw json.RawMessage) (any, error) {
		var t task.Task
		if err := decodeParams(raw, &t); err != nil {
			return nil, err
		}
		if t.Name == "" {
			return nil, jsonrpc.InvalidParams("name is required")
		}

		return svc.CreateTask(ctx, t)
	})

	srv.Register(jsonrpc.MethodTaskGet, func(ctx context.Context, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw)
		if err != nil {
			return nil, err
		}

		return svc.GetTask(ctx, id)
	})

	srv.Register(jsonrpc.MethodTaskList, func(ctx context.Context, raw json.RawMessage) (any, error) {
		params, err := decodeList(raw)
		if err != nil {
			return nil, err
		}

		return svc.ListTasks(ctx, manager.PageMetadata{
			Offset:   params.Offset,
			Limit:    params.Limit,
			Metadata: params.Metadata,
		})
	})

	srv.Register(jsonrpc.MethodTaskUpdate, func(ctx context.Context, raw json.RawMessage) (any, error) {
		var t task.Task
		if err := decodeParams(raw, &t); err != nil {
			return nil, err
		}
		if t.ID == "" {
			return nil, jsonrpc.InvalidParams("id is required")
		}

		return svc.UpdateTask(ctx, t)
	})

	srv.Register(jsonrpc.MethodTaskDelete, func(ctx context.Context, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw)
		if err != nil {
			return nil, err
		}
		if err := svc.DeleteTask(ctx, id); err != nil {
			return nil, err
		}

		return jsonrpc.NewAck(), nil
	})

	srv.Register(jsonrpc.MethodTaskStart, func(ctx context.Context, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw)
		if err != nil {
			return nil, err
		}
		if err := svc.StartTask(ctx, id); err != nil {
			return nil, err
		}

		return jsonrpc.NewAck(), nil
	})

	srv.Register(jsonrpc.MethodTaskStop, func(ctx context.Context, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw)
		if err != nil {
			return nil, err
		}
		if err := svc.StopTask(ctx, id); err != nil {
			return nil, err
		}

		return jsonrpc.NewAck(), nil
	})

	srv.Register(jsonrpc.MethodTaskResults, func(ctx context.Context, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw)
		if err != nil {
			return nil, err
		}
		results, err := svc.GetTaskResults(ctx, id)
		if err != nil {
			return nil, err
		}

		return jsonrpc.ResultsResult{Results: results}, nil
	})

	srv.Register(jsonrpc.MethodTaskMetrics, func(ctx context.Context, raw json.RawMessage) (any, error) {
		params, err := decodeEntityList(raw)
		if err != nil {
			return nil, err
		}

		return svc.GetTaskMetrics(ctx, params.ID, params.Offset, params.Limit)
	})
}

func registerPropletMethods(srv *jsonrpc.Server, svc manager.Service) {
	srv.Register(jsonrpc.MethodPropletGet, func(ctx context.Context, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw)
		if err != nil {
			return nil, err
		}

		return svc.GetProplet(ctx, id)
	})

	srv.Register(jsonrpc.MethodPropletList, func(ctx context.Context, raw json.RawMessage) (any, error) {
		params, err := decodeList(raw)
		if err != nil {
			return nil, err
		}

		return svc.ListProplets(ctx, params.Offset, params.Limit, params.Status)
	})

	srv.Register(jsonrpc.MethodPropletDelete, func(ctx context.Context, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw)
		if err != nil {
			return nil, err
		}
		if err := svc.DeleteProplet(ctx, id); err != nil {
			return nil, err
		}

		return jsonrpc.NewAck(), nil
	})

	srv.Register(jsonrpc.MethodPropletSDF, func(ctx context.Context, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw)
		if err != nil {
			return nil, err
		}

		return svc.GetPropletSDF(ctx, id)
	})

	srv.Register(jsonrpc.MethodPropletMetrics, func(ctx context.Context, raw json.RawMessage) (any, error) {
		params, err := decodeEntityList(raw)
		if err != nil {
			return nil, err
		}

		return svc.GetPropletMetrics(ctx, params.ID, params.Offset, params.Limit)
	})

	srv.Register(jsonrpc.MethodPropletAliveHistory, func(ctx context.Context, raw json.RawMessage) (any, error) {
		params, err := decodeEntityList(raw)
		if err != nil {
			return nil, err
		}

		return svc.GetPropletAliveHistory(ctx, params.ID, params.Offset, params.Limit)
	})
}

func registerJobMethods(srv *jsonrpc.Server, svc manager.Service) {
	srv.Register(jsonrpc.MethodJobCreate, func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params jsonrpc.JobParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		if len(params.Tasks) == 0 {
			return nil, jsonrpc.InvalidParams("tasks must not be empty")
		}

		jobID, tasks, err := svc.CreateJob(ctx, params.Name, params.Tasks, params.ExecutionMode)
		if err != nil {
			return nil, err
		}

		return jsonrpc.JobResult{JobID: jobID, Tasks: tasks}, nil
	})

	srv.Register(jsonrpc.MethodJobGet, func(ctx context.Context, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw)
		if err != nil {
			return nil, err
		}
		tasks, err := svc.GetJob(ctx, id)
		if err != nil {
			return nil, err
		}

		return jsonrpc.JobResult{JobID: id, Tasks: tasks}, nil
	})

	srv.Register(jsonrpc.MethodJobList, func(ctx context.Context, raw json.RawMessage) (any, error) {
		params, err := decodeList(raw)
		if err != nil {
			return nil, err
		}

		return svc.ListJobs(ctx, params.Offset, params.Limit, params.Status)
	})

	srv.Register(jsonrpc.MethodJobStart, func(ctx context.Context, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw)
		if err != nil {
			return nil, err
		}
		if err := svc.StartJob(ctx, id); err != nil {
			return nil, err
		}

		return jsonrpc.NewAck(), nil
	})

	srv.Register(jsonrpc.MethodJobStop, func(ctx context.Context, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw)
		if err != nil {
			return nil, err
		}
		if err := svc.StopJob(ctx, id); err != nil {
			return nil, err
		}

		return jsonrpc.NewAck(), nil
	})

	srv.Register(jsonrpc.MethodWorkflowCreate, func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params jsonrpc.WorkflowParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		if len(params.Tasks) == 0 {
			return nil, jsonrpc.InvalidParams("tasks must not be empty")
		}

		return svc.CreateWorkflow(ctx, params.Tasks)
	})
}
