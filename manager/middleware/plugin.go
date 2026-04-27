package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/plugin"
	"github.com/absmach/propeller/pkg/task"
)

type authKey struct{}

func ContextWithAuth(ctx context.Context, auth plugin.AuthContext) context.Context {
	return context.WithValue(ctx, authKey{}, auth)
}

func authFromContext(ctx context.Context) plugin.AuthContext {
	v, _ := ctx.Value(authKey{}).(plugin.AuthContext)

	return v
}

type pluginMiddleware struct {
	manager.Service

	registry plugin.Registry
	logger   *slog.Logger
}

func Plugin(registry plugin.Registry, logger *slog.Logger, svc manager.Service) manager.Service {
	return &pluginMiddleware{
		Service:  svc,
		registry: registry,
		logger:   logger,
	}
}

func (pm *pluginMiddleware) CreateTask(ctx context.Context, t task.Task) (task.Task, error) {
	info := toTaskInfo(t)

	if err := pm.authorize(ctx, plugin.ActionCreate, info); err != nil {
		return task.Task{}, err
	}

	enriched := pm.enrich(ctx, info)
	applyEnrichment(&t, enriched)

	return pm.Service.CreateTask(ctx, t)
}

func (pm *pluginMiddleware) UpdateTask(ctx context.Context, t task.Task) (task.Task, error) {
	if err := pm.authorize(ctx, plugin.ActionUpdate, toTaskInfo(t)); err != nil {
		return task.Task{}, err
	}

	return pm.Service.UpdateTask(ctx, t)
}

func (pm *pluginMiddleware) DeleteTask(ctx context.Context, taskID string) error {
	t, err := pm.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	if err := pm.authorize(ctx, plugin.ActionDelete, toTaskInfo(t)); err != nil {
		return err
	}

	return pm.Service.DeleteTask(ctx, taskID)
}

func (pm *pluginMiddleware) StartTask(ctx context.Context, taskID string) error {
	t, err := pm.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	info := toTaskInfo(t)
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

	if err := pm.authorize(ctx, plugin.ActionStop, toTaskInfo(t)); err != nil {
		return err
	}

	return pm.Service.StopTask(ctx, taskID)
}

func (pm *pluginMiddleware) authorize(ctx context.Context, action plugin.Action, info plugin.TaskInfo) error {
	plugins := pm.registry.List()
	if len(plugins) == 0 {
		return nil
	}

	req := plugin.AuthorizeRequest{
		Context: withAction(authFromContext(ctx), action),
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
	auth := authFromContext(ctx)

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
		if len(resp.Inputs) > 0 {
			merged.Inputs = resp.Inputs
		}
	}

	return merged
}

func (pm *pluginMiddleware) notifyStart(ctx context.Context, info plugin.TaskInfo) {
	plugins := pm.registry.List()
	for _, p := range plugins {
		go func(pl plugin.Plugin) {
			if err := pl.OnTaskStart(ctx, plugin.TaskEvent{Task: info}); err != nil {
				pm.logger.WarnContext(ctx, "plugin on_task_start failed", "plugin", pl.Name(), "error", err)
			}
		}(p)
	}
}

func withAction(auth plugin.AuthContext, action plugin.Action) plugin.AuthContext {
	auth.Action = action

	return auth
}

func toTaskInfo(t task.Task) plugin.TaskInfo {
	return plugin.TaskInfo{
		ID:        t.ID,
		Name:      t.Name,
		Kind:      string(t.Kind),
		ImageURL:  t.ImageURL,
		Inputs:    []string(t.Inputs),
		CLIArgs:   t.CLIArgs,
		Env:       t.Env,
		PropletID: t.PropletID,
		Priority:  t.Priority,
		Daemon:    t.Daemon,
		Encrypted: t.Encrypted,
		CreatedAt: t.CreatedAt,
	}
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
	if len(e.Inputs) > 0 {
		t.Inputs = task.FlexStrings(e.Inputs)
	}
}
