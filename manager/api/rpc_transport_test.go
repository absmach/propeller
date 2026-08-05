// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/absmach/propeller/manager/mocks"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/jsonrpc"
	"github.com/absmach/propeller/pkg/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func postRPC(t *testing.T, url, contentType, body string) (status int, payload string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, url+"/rpc", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, string(data)
}

func TestRPCEndpoint(t *testing.T) {
	t.Parallel()

	taskID := uuid.NewString()

	cases := []struct {
		desc        string
		contentType string
		body        string
		setup       func(svc *mocks.MockService)
		status      int
		want        string
	}{
		{
			desc:        "a call returns its result",
			contentType: "application/json",
			body:        `{"jsonrpc":"2.0","method":"task.start","params":{"id":"` + taskID + `"},"id":1}`,
			setup: func(svc *mocks.MockService) {
				svc.On("StartTask", mock.Anything, taskID).Return(nil)
			},
			status: http.StatusOK,
			want:   `{"jsonrpc":"2.0","result":{"ok":true},"id":1}`,
		},
		{
			desc:        "a service error becomes a jsonrpc error with a 200 status",
			contentType: "application/json",
			body:        `{"jsonrpc":"2.0","method":"task.get","params":{"id":"` + taskID + `"},"id":2}`,
			setup: func(svc *mocks.MockService) {
				svc.On("GetTask", mock.Anything, taskID).Return(task.Task{}, pkgerrors.ErrNotFound)
			},
			status: http.StatusOK,
			want:   `{"jsonrpc":"2.0","error":{"code":-32001,"message":"not found"},"id":2}`,
		},
		{
			desc:        "malformed json returns a parse error",
			contentType: "application/json",
			body:        `{"jsonrpc":"2.0",`,
			setup:       func(_ *mocks.MockService) {},
			status:      http.StatusOK,
			want:        `{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error"},"id":null}`,
		},
		{
			desc:        "an unknown method is reported",
			contentType: "application/json",
			body:        `{"jsonrpc":"2.0","method":"task.explode","id":3}`,
			setup:       func(_ *mocks.MockService) {},
			status:      http.StatusOK,
			want:        `{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found","data":"task.explode"},"id":3}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			ts, svc := newServer(t)
			defer ts.Close()
			tc.setup(svc)

			status, body := postRPC(t, ts.URL, tc.contentType, tc.body)
			assert.Equal(t, tc.status, status)
			assert.JSONEq(t, tc.want, body)
		})
	}
}

func TestRPCEndpointRejectsWrongContentType(t *testing.T) {
	t.Parallel()

	ts, _ := newServer(t)
	defer ts.Close()

	status, _ := postRPC(t, ts.URL, "text/plain", `{"jsonrpc":"2.0","method":"task.list","id":1}`)
	assert.Equal(t, http.StatusBadRequest, status)
}

func TestRPCEndpointNotificationReturnsNoContent(t *testing.T) {
	t.Parallel()

	taskID := uuid.NewString()
	ts, svc := newServer(t)
	defer ts.Close()
	svc.On("StartTask", mock.Anything, taskID).Return(nil)

	status, body := postRPC(t, ts.URL, "application/json",
		`{"jsonrpc":"2.0","method":"task.start","params":{"id":"`+taskID+`"}}`)

	assert.Equal(t, http.StatusNoContent, status)
	assert.Empty(t, body)
	svc.AssertExpectations(t)
}

func TestRPCEndpointBatch(t *testing.T) {
	t.Parallel()

	taskID := uuid.NewString()
	ts, svc := newServer(t)
	defer ts.Close()
	svc.On("StartTask", mock.Anything, taskID).Return(nil)
	svc.On("StopTask", mock.Anything, taskID).Return(nil)

	body := `[{"jsonrpc":"2.0","method":"task.start","params":{"id":"` + taskID + `"},"id":1},` +
		`{"jsonrpc":"2.0","method":"task.stop","params":{"id":"` + taskID + `"},"id":2}]`

	status, out := postRPC(t, ts.URL, "application/json", body)
	assert.Equal(t, http.StatusOK, status)

	var responses []jsonrpc.Response
	require.NoError(t, json.Unmarshal([]byte(out), &responses))
	require.Len(t, responses, 2)
	for _, r := range responses {
		assert.Nil(t, r.Error)
	}
	svc.AssertExpectations(t)
}

func TestRESTRoutesStillWork(t *testing.T) {
	t.Parallel()

	taskID := uuid.NewString()
	ts, svc := newServer(t)
	defer ts.Close()
	svc.On("GetTask", mock.Anything, taskID).Return(task.Task{ID: taskID, Name: "t"}, nil)

	resp, err := http.Get(ts.URL + "/tasks/" + taskID)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	svc.AssertExpectations(t)
}
