// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"testing"

	managerapi "github.com/absmach/propeller/manager/api"
	"github.com/absmach/propeller/manager/mocks"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/jsonrpc"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRPCServerMethods(t *testing.T) {
	t.Parallel()

	svc := new(mocks.MockService)
	methods := managerapi.NewRPCServer(svc).Methods()

	for _, method := range []string{
		jsonrpc.MethodTaskCreate,
		jsonrpc.MethodTaskGet,
		jsonrpc.MethodTaskList,
		jsonrpc.MethodTaskUpdate,
		jsonrpc.MethodTaskDelete,
		jsonrpc.MethodTaskStart,
		jsonrpc.MethodTaskStop,
		jsonrpc.MethodTaskResults,
		jsonrpc.MethodTaskMetrics,
		jsonrpc.MethodPropletGet,
		jsonrpc.MethodPropletList,
		jsonrpc.MethodPropletDelete,
		jsonrpc.MethodPropletSDF,
		jsonrpc.MethodPropletMetrics,
		jsonrpc.MethodPropletAliveHistory,
		jsonrpc.MethodJobCreate,
		jsonrpc.MethodJobGet,
		jsonrpc.MethodJobList,
		jsonrpc.MethodJobStart,
		jsonrpc.MethodJobStop,
		jsonrpc.MethodWorkflowCreate,
	} {
		assert.Contains(t, methods, method)
	}
}

func TestRPCDispatch(t *testing.T) {
	t.Parallel()

	taskID := uuid.NewString()
	propletID := uuid.NewString()

	cases := []struct {
		desc    string
		payload string
		setup   func(svc *mocks.MockService)
		want    string
	}{
		{
			desc:    "task.get returns the task",
			payload: `{"jsonrpc":"2.0","method":"task.get","params":{"id":"` + taskID + `"},"id":1}`,
			setup: func(svc *mocks.MockService) {
				svc.On("GetTask", mock.Anything, taskID).Return(task.Task{ID: taskID, Name: "t"}, nil)
			},
			want: `{"jsonrpc":"2.0","result":{"id":"` + taskID + `","name":"t","state":0,"cli_args":null,"daemon":false,"encrypted":false,"start_time":"0001-01-01T00:00:00Z","finish_time":"0001-01-01T00:00:00Z","created_at":"0001-01-01T00:00:00Z","updated_at":"0001-01-01T00:00:00Z","next_run":"0001-01-01T00:00:00Z"},"id":1}`,
		},
		{
			desc:    "task.start acknowledges",
			payload: `{"jsonrpc":"2.0","method":"task.start","params":{"id":"` + taskID + `"},"id":2}`,
			setup: func(svc *mocks.MockService) {
				svc.On("StartTask", mock.Anything, taskID).Return(nil)
			},
			want: `{"jsonrpc":"2.0","result":{"ok":true},"id":2}`,
		},
		{
			desc:    "task.stop acknowledges",
			payload: `{"jsonrpc":"2.0","method":"task.stop","params":{"id":"` + taskID + `"},"id":3}`,
			setup: func(svc *mocks.MockService) {
				svc.On("StopTask", mock.Anything, taskID).Return(nil)
			},
			want: `{"jsonrpc":"2.0","result":{"ok":true},"id":3}`,
		},
		{
			desc:    "task.delete acknowledges",
			payload: `{"jsonrpc":"2.0","method":"task.delete","params":{"id":"` + taskID + `"},"id":4}`,
			setup: func(svc *mocks.MockService) {
				svc.On("DeleteTask", mock.Anything, taskID).Return(nil)
			},
			want: `{"jsonrpc":"2.0","result":{"ok":true},"id":4}`,
		},
		{
			desc:    "task.results wraps the results",
			payload: `{"jsonrpc":"2.0","method":"task.results","params":{"id":"` + taskID + `"},"id":5}`,
			setup: func(svc *mocks.MockService) {
				svc.On("GetTaskResults", mock.Anything, taskID).Return("42", nil)
			},
			want: `{"jsonrpc":"2.0","result":{"results":"42"},"id":5}`,
		},
		{
			desc:    "a missing task maps to the not found code",
			payload: `{"jsonrpc":"2.0","method":"task.get","params":{"id":"` + taskID + `"},"id":6}`,
			setup: func(svc *mocks.MockService) {
				svc.On("GetTask", mock.Anything, taskID).Return(task.Task{}, pkgerrors.ErrNotFound)
			},
			want: `{"jsonrpc":"2.0","error":{"code":-32001,"message":"not found"},"id":6}`,
		},
		{
			desc:    "a missing id is rejected before the service is called",
			payload: `{"jsonrpc":"2.0","method":"task.start","params":{},"id":7}`,
			setup:   func(_ *mocks.MockService) {},
			want:    `{"jsonrpc":"2.0","error":{"code":-32602,"message":"Invalid params","data":"id is required"},"id":7}`,
		},
		{
			desc:    "task.create requires a name",
			payload: `{"jsonrpc":"2.0","method":"task.create","params":{},"id":8}`,
			setup:   func(_ *mocks.MockService) {},
			want:    `{"jsonrpc":"2.0","error":{"code":-32602,"message":"Invalid params","data":"name is required"},"id":8}`,
		},
		{
			desc:    "malformed params are rejected without leaking internals",
			payload: `{"jsonrpc":"2.0","method":"task.get","params":{"id":5},"id":9}`,
			setup:   func(_ *mocks.MockService) {},
			want:    `{"jsonrpc":"2.0","error":{"code":-32602,"message":"Invalid params","data":"params could not be decoded"},"id":9}`,
		},
		{
			desc:    "proplet.delete acknowledges",
			payload: `{"jsonrpc":"2.0","method":"proplet.delete","params":{"id":"` + propletID + `"},"id":10}`,
			setup: func(svc *mocks.MockService) {
				svc.On("DeleteProplet", mock.Anything, propletID).Return(nil)
			},
			want: `{"jsonrpc":"2.0","result":{"ok":true},"id":10}`,
		},
		{
			desc:    "workflow.create requires tasks",
			payload: `{"jsonrpc":"2.0","method":"workflow.create","params":{"tasks":[]},"id":11}`,
			setup:   func(_ *mocks.MockService) {},
			want:    `{"jsonrpc":"2.0","error":{"code":-32602,"message":"Invalid params","data":"tasks must not be empty"},"id":11}`,
		},
		{
			desc:    "job.get wraps the job id with its tasks",
			payload: `{"jsonrpc":"2.0","method":"job.get","params":{"id":"job-1"},"id":12}`,
			setup: func(svc *mocks.MockService) {
				svc.On("GetJob", mock.Anything, "job-1").Return([]task.Task{}, nil)
			},
			want: `{"jsonrpc":"2.0","result":{"job_id":"job-1","tasks":[]},"id":12}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			svc := new(mocks.MockService)
			tc.setup(svc)

			out := managerapi.NewRPCServer(svc).Handle(context.Background(), []byte(tc.payload))
			require.NotNil(t, out)
			assert.JSONEq(t, tc.want, string(out))
		})
	}
}

func TestRPCListAppliesDefaultLimit(t *testing.T) {
	t.Parallel()

	svc := new(mocks.MockService)
	svc.On("ListProplets", mock.Anything, uint64(0), uint64(100), "").
		Return(proplet.PropletPage{Offset: 0, Limit: 100}, nil)

	out := managerapi.NewRPCServer(svc).Handle(
		context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"proplet.list","id":1}`),
	)

	var resp jsonrpc.Response
	require.NoError(t, json.Unmarshal(out, &resp))
	require.Nil(t, resp.Error)
	svc.AssertExpectations(t)
}

func TestRPCBatchAcrossServices(t *testing.T) {
	t.Parallel()

	taskID := uuid.NewString()
	svc := new(mocks.MockService)
	svc.On("StartTask", mock.Anything, taskID).Return(nil)
	svc.On("GetTaskResults", mock.Anything, taskID).Return("done", nil)

	payload := `[{"jsonrpc":"2.0","method":"task.start","params":{"id":"` + taskID + `"},"id":1},` +
		`{"jsonrpc":"2.0","method":"task.results","params":{"id":"` + taskID + `"},"id":2}]`

	out := managerapi.NewRPCServer(svc).Handle(context.Background(), []byte(payload))
	assert.JSONEq(
		t,
		`[{"jsonrpc":"2.0","result":{"ok":true},"id":1},{"jsonrpc":"2.0","result":{"results":"done"},"id":2}]`,
		string(out),
	)
	svc.AssertExpectations(t)
}

func TestRPCNotificationProducesNoResponse(t *testing.T) {
	t.Parallel()

	taskID := uuid.NewString()
	svc := new(mocks.MockService)
	svc.On("StartTask", mock.Anything, taskID).Return(nil)

	out := managerapi.NewRPCServer(svc).Handle(
		context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"task.start","params":{"id":"`+taskID+`"}}`),
	)

	assert.Nil(t, out)
	svc.AssertExpectations(t)
}
