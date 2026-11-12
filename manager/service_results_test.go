// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package manager_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/mqtt"
	mqttmocks "github.com/absmach/propeller/pkg/mqtt/mocks"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testResultsTopic  = testBaseTopic + "/control/proplet/results"
	testDaemonResults = "started at port 8222"
)

func TestUpdateResultsHandlerKeepsDaemonRunning(t *testing.T) {
	t.Parallel()

	svc, resultsHandler := newServiceWithResultsHandler(t)
	ctx := context.Background()

	cases := []struct {
		desc          string
		daemon        bool
		errMsg        string
		expectedState task.State
	}{
		{
			desc:          "daemon start report leaves task running",
			daemon:        true,
			expectedState: task.Running,
		},
		{
			desc:          "daemon start failure marks task failed",
			daemon:        true,
			errMsg:        "failed to start daemon",
			expectedState: task.Failed,
		},
		{
			desc:          "non-daemon result completes task",
			daemon:        false,
			expectedState: task.Completed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			created, err := svc.CreateTask(ctx, task.Task{
				Name:   tc.desc,
				State:  task.Running,
				Daemon: tc.daemon,
			})
			require.NoError(t, err)

			err = resultsHandler(testResultsTopic, map[string]any{
				"task_id":    created.ID,
				"proplet_id": "proplet-1",
				"results":    testDaemonResults,
				"error":      tc.errMsg,
			})
			require.NoError(t, err)

			got, err := svc.GetTask(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedState, got.State)
			assert.Equal(t, testDaemonResults, got.Results)

			if tc.expectedState == task.Running {
				assert.True(t, got.FinishTime.IsZero(), "running daemon must not have a finish time")
			} else {
				assert.False(t, got.FinishTime.IsZero(), "terminal task must have a finish time")
			}
		})
	}
}

func newServiceWithResultsHandler(t *testing.T) (manager.Service, mqtt.Handler) {
	t.Helper()

	repos, err := storage.NewRepositories(storage.Config{Type: "memory"})
	require.NoError(t, err)

	pubsub := mqttmocks.NewMockPubSub(t)
	var resultsHandler mqtt.Handler
	pubsub.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			if handler, ok := args.Get(2).(mqtt.Handler); ok && strings.HasSuffix(args.String(1), "/#") {
				resultsHandler = handler
			}
		}).
		Return(nil).
		Maybe()

	svc, _, _ := manager.NewService(repos, scheduler.NewRoundRobin(), pubsub, "test-tenant", "test-channel", "", slog.Default(), nil)
	require.NoError(t, svc.Subscribe(context.Background()))
	require.NotNil(t, resultsHandler)

	return svc, resultsHandler
}
