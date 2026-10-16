// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package jsonrpc_test

import (
	"encoding/json"
	"testing"

	"github.com/absmach/propeller/pkg/jsonrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestUnmarshal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc           string
		payload        string
		method         string
		id             string
		isNotification bool
		decodeErr      bool
		validateErr    error
	}{
		{
			desc:    "request with object params and string id",
			payload: `{"jsonrpc":"2.0","method":"task.start","params":{"id":"abc"},"id":"req-1"}`,
			method:  "task.start",
			id:      "req-1",
		},
		{
			desc:    "request with array params and numeric id",
			payload: `{"jsonrpc":"2.0","method":"task.stop","params":["abc"],"id":7}`,
			method:  "task.stop",
			id:      "7",
		},
		{
			desc:           "notification has no id",
			payload:        `{"jsonrpc":"2.0","method":"proplet.alive"}`,
			method:         "proplet.alive",
			isNotification: true,
		},
		{
			desc:    "null id is not a notification",
			payload: `{"jsonrpc":"2.0","method":"task.list","id":null}`,
			method:  "task.list",
			id:      "null",
		},
		{
			desc:        "wrong version is rejected",
			payload:     `{"jsonrpc":"1.0","method":"task.list","id":1}`,
			method:      "task.list",
			id:          "1",
			validateErr: jsonrpc.ErrInvalidVersion,
		},
		{
			desc:        "missing method is rejected",
			payload:     `{"jsonrpc":"2.0","id":1}`,
			id:          "1",
			validateErr: jsonrpc.ErrMissingMethod,
		},
		{
			desc:        "scalar params are rejected",
			payload:     `{"jsonrpc":"2.0","method":"task.list","params":5,"id":1}`,
			method:      "task.list",
			id:          "1",
			validateErr: jsonrpc.ErrInvalidParams,
		},
		{
			desc:      "boolean id is rejected",
			payload:   `{"jsonrpc":"2.0","method":"task.list","id":true}`,
			decodeErr: true,
		},
		{
			desc:      "object id is rejected",
			payload:   `{"jsonrpc":"2.0","method":"task.list","id":{"a":1}}`,
			decodeErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			var req jsonrpc.Request
			err := json.Unmarshal([]byte(tc.payload), &req)
			if tc.decodeErr {
				require.Error(t, err)

				return
			}
			require.NoError(t, err)

			assert.Equal(t, tc.method, req.Method)
			assert.Equal(t, tc.isNotification, req.IsNotification())
			if !tc.isNotification {
				require.NotNil(t, req.ID)
				assert.Equal(t, tc.id, req.ID.String())
			}

			err = req.Validate()
			if tc.validateErr != nil {
				assert.ErrorIs(t, err, tc.validateErr)

				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestRequestUnmarshalParams(t *testing.T) {
	t.Parallel()

	type params struct {
		ID     string   `json:"id"`
		Inputs []string `json:"inputs"`
	}

	cases := []struct {
		desc    string
		payload string
		want    params
	}{
		{
			desc:    "object params are decoded",
			payload: `{"jsonrpc":"2.0","method":"m","params":{"id":"a","inputs":["x"]},"id":1}`,
			want:    params{ID: "a", Inputs: []string{"x"}},
		},
		{
			desc:    "absent params leave the target untouched",
			payload: `{"jsonrpc":"2.0","method":"m","id":1}`,
			want:    params{},
		},
		{
			desc:    "null params leave the target untouched",
			payload: `{"jsonrpc":"2.0","method":"m","params":null,"id":1}`,
			want:    params{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			var req jsonrpc.Request
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &req))

			var got params
			require.NoError(t, req.UnmarshalParams(&got))
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNewRequest(t *testing.T) {
	t.Parallel()

	req, err := jsonrpc.NewRequest(jsonrpc.StringID("req-1"), "task.start", map[string]string{"id": "abc"})
	require.NoError(t, err)

	data, err := json.Marshal(req)
	require.NoError(t, err)

	assert.JSONEq(t, `{"jsonrpc":"2.0","method":"task.start","params":{"id":"abc"},"id":"req-1"}`, string(data))
}

func TestNewRequestNotificationOmitsID(t *testing.T) {
	t.Parallel()

	req, err := jsonrpc.NewRequest(nil, "proplet.alive", nil)
	require.NoError(t, err)
	assert.True(t, req.IsNotification())

	data, err := json.Marshal(req)
	require.NoError(t, err)
	assert.JSONEq(t, `{"jsonrpc":"2.0","method":"proplet.alive"}`, string(data))
}

func TestResponseRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc     string
		response *jsonrpc.Response
		want     string
	}{
		{
			desc: "success response carries a result",
			response: func() *jsonrpc.Response {
				resp, err := jsonrpc.NewResponse(jsonrpc.StringID("req-1"), map[string]bool{"started": true})
				require.NoError(t, err)

				return resp
			}(),
			want: `{"jsonrpc":"2.0","result":{"started":true},"id":"req-1"}`,
		},
		{
			desc:     "error response carries an error",
			response: jsonrpc.NewErrorResponse(jsonrpc.NumberID(3), jsonrpc.MethodNotFound("nope")),
			want:     `{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found","data":"nope"},"id":3}`,
		},
		{
			desc:     "error response without an id serialises a null id",
			response: jsonrpc.NewErrorResponse(nil, jsonrpc.ParseError(nil)),
			want:     `{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error"},"id":null}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.response)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(data))
		})
	}
}

func TestResponseUnmarshalResult(t *testing.T) {
	t.Parallel()

	t.Run("result is decoded", func(t *testing.T) {
		t.Parallel()

		var resp jsonrpc.Response
		require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","result":{"started":true},"id":1}`), &resp))

		var out struct {
			Started bool `json:"started"`
		}
		require.NoError(t, resp.UnmarshalResult(&out))
		assert.True(t, out.Started)
	})

	t.Run("error responses surface the error", func(t *testing.T) {
		t.Parallel()

		var resp jsonrpc.Response
		require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","error":{"code":-32001,"message":"not found"},"id":1}`), &resp))

		var out map[string]any
		err := resp.UnmarshalResult(&out)
		require.Error(t, err)

		var rpcErr *jsonrpc.Error
		require.ErrorAs(t, err, &rpcErr)
		assert.Equal(t, jsonrpc.CodeNotFound, rpcErr.Code)
	})
}

func TestIDHelpers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "abc", jsonrpc.StringID("abc").String())
	assert.Equal(t, "42", jsonrpc.NumberID(42).String())
	assert.True(t, jsonrpc.NullID().IsNull())
	assert.False(t, jsonrpc.StringID("abc").IsNull())
}
