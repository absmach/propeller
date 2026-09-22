// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package manager_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/jsonrpc"
	mqttmocks "github.com/absmach/propeller/pkg/mqtt/mocks"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func captureControlPublish(t *testing.T, suffix string, opts ...manager.Option) (svc manager.Service, captured func() map[string]any) {
	t.Helper()

	repos, err := storage.NewRepositories(storage.Config{Type: "memory"})
	require.NoError(t, err)

	var capturedMsg map[string]any
	pubsub := mqttmocks.NewMockPubSub(t)
	pubsub.On("Publish", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			topic, _ := args.Get(1).(string)
			if !strings.HasSuffix(topic, suffix) {
				return
			}
			raw, err := json.Marshal(args.Get(2))
			require.NoError(t, err)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(raw, &decoded))
			capturedMsg = decoded
		}).
		Return(nil).Maybe()
	pubsub.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	pubsub.On("Unsubscribe", mock.Anything, mock.Anything).Return(nil).Maybe()
	pubsub.On("Disconnect", mock.Anything).Return(nil).Maybe()

	service, _, _ := manager.NewService(
		repos, scheduler.NewRoundRobin(), pubsub,
		"test-tenant", "test-channel", "", slog.Default(), nil,
		opts...,
	)

	return service, func() map[string]any { return capturedMsg }
}

func TestPublishStopEncoding(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc    string
		opts    []manager.Option
		wrapped bool
	}{
		{
			desc:    "legacy flat payload by default",
			wrapped: false,
		},
		{
			desc:    "jsonrpc envelope when the control plane is enabled",
			opts:    []manager.Option{manager.WithJSONRPCControlPlane(true)},
			wrapped: true,
		},
		{
			desc:    "explicitly disabled stays flat",
			opts:    []manager.Option{manager.WithJSONRPCControlPlane(false)},
			wrapped: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			svc, captured := captureControlPublish(t, "/control/manager/stop", tc.opts...)

			created, err := svc.CreateTask(context.Background(), task.Task{Name: "t", Broadcast: true})
			require.NoError(t, err)
			require.NoError(t, svc.StopTask(context.Background(), created.ID))

			payload := captured()
			require.NotNil(t, payload, "no stop message was published")

			if !tc.wrapped {
				assert.NotContains(t, payload, "jsonrpc")
				assert.Equal(t, created.ID, payload["id"])

				return
			}

			assert.Equal(t, jsonrpc.Version, payload["jsonrpc"])
			assert.Equal(t, jsonrpc.MethodTaskStop, payload["method"])
			assert.Equal(t, created.ID, payload["id"])

			params, ok := payload["params"].(map[string]any)
			require.True(t, ok, "params must be an object")
			assert.Equal(t, created.ID, params["id"])
		})
	}
}

func TestPublishStartEncoding(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc    string
		opts    []manager.Option
		wrapped bool
	}{
		{
			desc:    "legacy flat payload by default",
			wrapped: false,
		},
		{
			desc:    "jsonrpc envelope when the control plane is enabled",
			opts:    []manager.Option{manager.WithJSONRPCControlPlane(true)},
			wrapped: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			svc, captured := captureControlPublish(t, "/control/manager/start", tc.opts...)

			created, err := svc.CreateTask(context.Background(), task.Task{Name: "t", Broadcast: true})
			require.NoError(t, err)
			require.NoError(t, svc.StartTask(context.Background(), created.ID))

			payload := captured()
			require.NotNil(t, payload, "no start message was published")

			if !tc.wrapped {
				assert.NotContains(t, payload, "jsonrpc")
				assert.Equal(t, created.ID, payload["id"])
				assert.Equal(t, "t", payload["name"])

				return
			}

			assert.Equal(t, jsonrpc.Version, payload["jsonrpc"])
			assert.Equal(t, jsonrpc.MethodTaskStart, payload["method"])

			params, ok := payload["params"].(map[string]any)
			require.True(t, ok, "params must be an object")
			assert.Equal(t, created.ID, params["id"])
			assert.Equal(t, "t", params["name"])
		})
	}
}
