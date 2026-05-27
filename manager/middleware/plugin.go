package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/plugin"
	"github.com/absmach/propeller/pkg/task"
)

type pluginMiddleware struct {
	manager.Service

	registry plugin.Registry
	logger   *slog.Logger
	wg       sync.WaitGroup
}

func Plugin(registry plugin.Registry, logger *slog.Logger, svc manager.Service) manager.Service {
	return &pluginMiddleware{
		Service:  svc,
		registry: registry,
		logger:   logger,
	}
}

func (pm *pluginMiddleware) CreateTask(ctx context.Context, t task.Task) (task.Task, error) {
	info := plugin.NewTaskInfo(t)

	if err := pm.authorize(ctx, plugin.ActionCreate, info); err != nil {
		return task.Task{}, err
	}

	enriched := pm.enrich(ctx, info)
	applyEnrichment(&t, enriched)

	return pm.Service.CreateTask(ctx, t)
}

func (pm *pluginMiddleware) UpdateTask(ctx context.Context, t task.Task) (task.Task, error) {
	if err := pm.authorize(ctx, plugin.ActionUpdate, plugin.NewTaskInfo(t)); err != nil {
		return task.Task{}, err
	}

	return pm.Service.UpdateTask(ctx, t)
}

func (pm *pluginMiddleware) DeleteTask(ctx context.Context, taskID string) error {
	t, err := pm.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	if err := pm.authorize(ctx, plugin.ActionDelete, plugin.NewTaskInfo(t)); err != nil {
		return err
	}

	return pm.Service.DeleteTask(ctx, taskID)
}

func (pm *pluginMiddleware) StartTask(ctx context.Context, taskID string) error {
	t, err := pm.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	info := plugin.NewTaskInfo(t)
	if err := pm.authorize(ctx, plugin.ActionStart, info); err != nil {
		return err
	}

	if err := pm.Service.StartTask(ctx, taskID); err != nil {
		return err
	}

	pm.notifyStart(ctx, info)

	return nil
}

func (pm *pluginMiddleware) StopTask(ctx context.Context, taskID string) error {
	t, err := pm.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	if err := pm.authorize(ctx, plugin.ActionStop, plugin.NewTaskInfo(t)); err != nil {
		return err
	}

	return pm.Service.StopTask(ctx, taskID)
}

func (pm *pluginMiddleware) CreateWorkflow(ctx context.Context, tasks []task.Task) ([]task.Task, error) {
	for i := range tasks {
		info := plugin.NewTaskInfo(tasks[i])
		if err := pm.authorize(ctx, plugin.ActionCreate, info); err != nil {
			return nil, err
		}
		enriched := pm.enrich(ctx, info)
		applyEnrichment(&tasks[i], enriched)
	}

	return pm.Service.CreateWorkflow(ctx, tasks)
}

func (pm *pluginMiddleware) CreateJob(ctx context.Context, name string, tasks []task.Task, executionMode string) (string, []task.Task, error) {
	for i := range tasks {
		info := plugin.NewTaskInfo(tasks[i])
		if err := pm.authorize(ctx, plugin.ActionCreate, info); err != nil {
			return "", nil, err
		}
		enriched := pm.enrich(ctx, info)
		applyEnrichment(&tasks[i], enriched)
	}

	return pm.Service.CreateJob(ctx, name, tasks, executionMode)
}

func (pm *pluginMiddleware) StartJob(ctx context.Context, jobID string) error {
	tasks, err := pm.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	for i := range tasks {
		info := plugin.NewTaskInfo(tasks[i])
		if err := pm.authorize(ctx, plugin.ActionStart, info); err != nil {
			return err
		}
	}

	return pm.Service.StartJob(ctx, jobID)
}

func (pm *pluginMiddleware) StopJob(ctx context.Context, jobID string) error {
	tasks, err := pm.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	for i := range tasks {
		if err := pm.authorize(ctx, plugin.ActionStop, plugin.NewTaskInfo(tasks[i])); err != nil {
			return err
		}
	}

	return pm.Service.StopJob(ctx, jobID)
}

func (pm *pluginMiddleware) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		pm.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		pm.logger.WarnContext(ctx, "shutdown timeout waiting for plugin notification goroutines")
	}

	return pm.Service.Shutdown(ctx)
}

func (pm *pluginMiddleware) authorize(ctx context.Context, action plugin.Action, info plugin.TaskInfo) error {
	plugins := pm.registry.List()
	if len(plugins) == 0 {
		return nil
	}

	req := plugin.AuthorizeRequest{
		Context: withAction(plugin.AuthFromContext(ctx), action),
		Task:    info,
	}

	for _, p := range plugins {
		resp, err := p.Authorize(ctx, req)
		if err != nil {
			pm.logger.ErrorContext(ctx, "plugin authorize failed", "plugin", p.Name(), "error", err)

			return fmt.Errorf("plugin %s authorize: %w", p.Name(), err)
		}
		if !resp.Allow {
			reason := resp.Reason
			if reason == "" {
				reason = "denied by plugin"
			}

			return errors.New(reason)
		}
	}

	return nil
}

func (pm *pluginMiddleware) enrich(ctx context.Context, info plugin.TaskInfo) plugin.EnrichResponse {
	plugins := pm.registry.List()
	merged := plugin.EnrichResponse{}
	auth := plugin.AuthFromContext(ctx)

	for _, p := range plugins {
		resp, err := p.Enrich(ctx, plugin.EnrichRequest{Context: auth, Task: info})
		if err != nil {
			pm.logger.WarnContext(ctx, "plugin enrich failed", "plugin", p.Name(), "error", err)

			continue
		}

		if len(resp.Env) > 0 {
			if merged.Env == nil {
				merged.Env = make(map[string]string, len(resp.Env))
			}
			maps.Copy(merged.Env, resp.Env)
		}
		if resp.Priority != nil {
			merged.Priority = resp.Priority
		}
		for _, v := range resp.Inputs {
			if !slices.Contains(merged.Inputs, v) {
				merged.Inputs = append(merged.Inputs, v)
			}
		}
		if resp.ImageURL != "" {
			merged.ImageURL = resp.ImageURL
		}
	}

	return merged
}

func (pm *pluginMiddleware) notifyStart(ctx context.Context, info plugin.TaskInfo) {
	detached := context.WithoutCancel(ctx)
	plugins := pm.registry.List()
	for _, p := range plugins {
		pm.wg.Go(func() {
			if err := p.OnTaskStart(detached, plugin.TaskEvent{Task: info}); err != nil {
				pm.logger.WarnContext(detached, "plugin on_task_start failed", "plugin", p.Name(), "error", err)
			}
		})
	}
}

func withAction(auth plugin.AuthContext, action plugin.Action) plugin.AuthContext {
	auth.Action = action

	return auth
}

func applyEnrichment(t *task.Task, e plugin.EnrichResponse) {
	if len(e.Env) > 0 {
		if t.Env == nil {
			t.Env = make(map[string]string, len(e.Env))
		}
		maps.Copy(t.Env, e.Env)
	}
	if e.Priority != nil {
		t.Priority = *e.Priority
	}
	for _, v := range e.Inputs {
		if !slices.Contains([]string(t.Inputs), v) {
			t.Inputs = append(t.Inputs, v)
		}
	}
	if e.ImageURL != "" {
		t.ImageURL = e.ImageURL
	}
}
