// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package manager_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/mqtt"
	mqttmocks "github.com/absmach/propeller/pkg/mqtt/mocks"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const regressionResultsTopic = testBaseTopic + "/control/proplet/results"

// newRegressionService wires an in-memory service and returns the MQTT handler
// plus captured stop-publish payloads and the repositories for direct setup.
func newRegressionService(t *testing.T) (manager.Service, mqtt.Handler, *[]any, *sync.Mutex, *storage.Repositories) {
	t.Helper()

	repos, err := storage.NewRepositories(storage.Config{Type: "memory"})
	require.NoError(t, err)

	var (
		handler mqtt.Handler
		stops   []any
		mu      sync.Mutex
	)
	pubsub := mqttmocks.NewMockPubSub(t)
	pubsub.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			if h, ok := args.Get(2).(mqtt.Handler); ok && strings.HasSuffix(args.String(1), "/#") {
				handler = h
			}
		}).
		Return(nil).Maybe()
	pubsub.On("Publish", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			if strings.HasSuffix(args.String(1), "/control/manager/stop") {
				mu.Lock()
				stops = append(stops, args.Get(2))
				mu.Unlock()
			}
		}).
		Return(nil).Maybe()

	svc, _, _ := manager.NewService(repos, scheduler.NewRoundRobin(), pubsub, "test-tenant", "test-channel", "", slog.Default(), nil)
	require.NoError(t, svc.Subscribe(context.Background()))
	require.NotNil(t, handler)

	return svc, handler, &stops, &mu, repos
}

func TestCompletionReleasesPropletTaskCount(t *testing.T) {
	ctx := context.Background()
	svc, handler, _, _, repos := newRegressionService(t)

	require.NoError(t, repos.Proplets.Create(ctx, proplet.Proplet{ID: "proplet-1", Alive: true, TaskCount: 1}))
	created, err := svc.CreateTask(ctx, task.Task{Name: "completed-task", State: task.Running})
	require.NoError(t, err)
	require.NoError(t, repos.TaskProplets.Create(ctx, created.ID, "proplet-1"))

	result := map[string]any{"task_id": created.ID, "results": "ok"}
	require.NoError(t, handler(regressionResultsTopic, result))

	got, err := svc.GetTask(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, task.Completed, got.State)

	p, err := repos.Proplets.Get(ctx, "proplet-1")
	require.NoError(t, err)
	assert.Zero(t, p.TaskCount, "completing a task must release its active-task slot")
	_, err = repos.TaskProplets.Get(ctx, created.ID)
	require.Error(t, err, "task-to-proplet pin must be removed on completion")

	// Duplicate result messages must not decrement twice.
	require.NoError(t, handler(regressionResultsTopic, result))
	p, err = repos.Proplets.Get(ctx, "proplet-1")
	require.NoError(t, err)
	assert.Zero(t, p.TaskCount)
}

func TestStartJobFailureStopsStartedTasks(t *testing.T) {
	ctx := context.Background()
	svc, _, stops, mu, repos := newRegressionService(t)

	require.NoError(t, repos.Proplets.Create(ctx, proplet.Proplet{
		ID: "proplet-1", Alive: true, AliveHistory: []time.Time{time.Now()},
	}))
	jobID, _, err := svc.CreateJob(ctx, "failing-job", []task.Task{
		{ID: "task-ok", Name: "starts", State: task.Pending},
		{ID: "task-bad", Name: "fails-to-start", State: task.Pending, PropletID: "missing-proplet"},
	}, manager.ExecutionModeParallel)
	require.NoError(t, err)

	err = svc.StartJob(ctx, jobID)
	require.Error(t, err, "the job must report the task that failed to start")

	mu.Lock()
	stopIDs := make([]string, len(*stops))
	for i, payload := range *stops {
		m, ok := payload.(map[string]any)
		require.True(t, ok)
		stopIDs[i], _ = m["id"].(string)
	}
	mu.Unlock()

	assert.Contains(t, stopIDs, "task-ok", "a sibling that started must be stopped on fail-fast")

	p, err := repos.Proplets.Get(ctx, "proplet-1")
	require.NoError(t, err)
	assert.Zero(t, p.TaskCount, "the stopped task must release its slot")
}

func TestConcurrentStartsDoNotLoseTaskCount(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, repos := newRegressionService(t)

	require.NoError(t, repos.Proplets.Create(ctx, proplet.Proplet{
		ID: "proplet-1", Alive: true, AliveHistory: []time.Time{time.Now()},
	}))

	const taskCount = 8
	ids := make([]string, 0, taskCount)
	for i := 0; i < taskCount; i++ {
		created, err := svc.CreateTask(ctx, task.Task{Name: "concurrent", State: task.Pending})
		require.NoError(t, err)
		ids = append(ids, created.ID)
	}

	start := make(chan struct{})
	errCh := make(chan error, taskCount)
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			if err := svc.StartTask(ctx, id); err != nil {
				errCh <- err
			}
		}(id)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	p, err := repos.Proplets.Get(ctx, "proplet-1")
	require.NoError(t, err)
	assert.Equal(t, uint64(taskCount), p.TaskCount)
}
