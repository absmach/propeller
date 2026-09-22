// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package jsonrpc_test

import (
	"encoding/json"
	"testing"

	"github.com/absmach/propeller/pkg/jsonrpc"
	"github.com/absmach/propeller/pkg/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMethodNamesAreNamespaced(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc   string
		method string
		want   string
	}{
		{desc: "task create", method: jsonrpc.MethodTaskCreate, want: "task.create"},
		{desc: "task start", method: jsonrpc.MethodTaskStart, want: "task.start"},
		{desc: "task stop", method: jsonrpc.MethodTaskStop, want: "task.stop"},
		{desc: "task results", method: jsonrpc.MethodTaskResults, want: "task.results"},
		{desc: "proplet list", method: jsonrpc.MethodPropletList, want: "proplet.list"},
		{desc: "proplet alive history", method: jsonrpc.MethodPropletAliveHistory, want: "proplet.aliveHistory"},
		{desc: "job create", method: jsonrpc.MethodJobCreate, want: "job.create"},
		{desc: "workflow create", method: jsonrpc.MethodWorkflowCreate, want: "workflow.create"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.method)
		})
	}
}

func TestParamShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc   string
		params any
		want   string
	}{
		{
			desc:   "id params",
			params: jsonrpc.IDParams{ID: "abc"},
			want:   `{"id":"abc"}`,
		},
		{
			desc:   "list params omit empty fields",
			params: jsonrpc.ListParams{Limit: 10},
			want:   `{"limit":10}`,
		},
		{
			desc:   "list params carry a status filter",
			params: jsonrpc.ListParams{Offset: 5, Limit: 10, Status: "alive"},
			want:   `{"offset":5,"limit":10,"status":"alive"}`,
		},
		{
			desc:   "entity list params always carry an id",
			params: jsonrpc.EntityListParams{ID: "abc"},
			want:   `{"id":"abc"}`,
		},
		{
			desc:   "ack is a stable shape",
			params: jsonrpc.NewAck(),
			want:   `{"ok":true}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.params)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(data))
		})
	}
}

func TestTaskCarryingParamsRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc   string
		params any
		decode func(t *testing.T, raw []byte) []task.Task
	}{
		{
			desc:   "workflow params",
			params: jsonrpc.WorkflowParams{Tasks: []task.Task{{Name: "a"}, {Name: "b"}}},
			decode: func(t *testing.T, raw []byte) []task.Task {
				t.Helper()
				var out jsonrpc.WorkflowParams
				require.NoError(t, json.Unmarshal(raw, &out))

				return out.Tasks
			},
		},
		{
			desc:   "job params",
			params: jsonrpc.JobParams{Name: "j", Tasks: []task.Task{{Name: "a"}, {Name: "b"}}, ExecutionMode: "parallel"},
			decode: func(t *testing.T, raw []byte) []task.Task {
				t.Helper()
				var out jsonrpc.JobParams
				require.NoError(t, json.Unmarshal(raw, &out))
				assert.Equal(t, "j", out.Name)
				assert.Equal(t, "parallel", out.ExecutionMode)

				return out.Tasks
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			raw, err := json.Marshal(tc.params)
			require.NoError(t, err)

			var envelope map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &envelope))
			require.Contains(t, envelope, "tasks")

			tasks := tc.decode(t, raw)
			require.Len(t, tasks, 2)
			assert.Equal(t, "a", tasks[0].Name)
			assert.Equal(t, "b", tasks[1].Name)
		})
	}
}

func TestParamsRoundTripThroughRequest(t *testing.T) {
	t.Parallel()

	req, err := jsonrpc.NewRequest(jsonrpc.NumberID(1), jsonrpc.MethodTaskMetrics, jsonrpc.EntityListParams{
		ID:     "abc",
		Offset: 2,
		Limit:  5,
	})
	require.NoError(t, err)

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded jsonrpc.Request
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, jsonrpc.MethodTaskMetrics, decoded.Method)

	var params jsonrpc.EntityListParams
	require.NoError(t, decoded.UnmarshalParams(&params))
	assert.Equal(t, jsonrpc.EntityListParams{ID: "abc", Offset: 2, Limit: 5}, params)
}
