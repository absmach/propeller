// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package jsonrpc_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/jsonrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testServer() *jsonrpc.Server {
	srv := jsonrpc.NewServer()
	srv.Register("echo", func(_ context.Context, params json.RawMessage) (any, error) {
		var in struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, jsonrpc.InvalidParams(err.Error())
		}

		return map[string]string{"value": in.Value}, nil
	})
	srv.Register("boom", func(_ context.Context, _ json.RawMessage) (any, error) {
		return nil, pkgerrors.ErrNotFound
	})
	srv.Register("noop", func(_ context.Context, _ json.RawMessage) (any, error) {
		var empty any

		return empty, nil
	})

	return srv
}

func TestServerHandle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc     string
		payload  string
		want     string
		wantNone bool
	}{
		{
			desc:    "successful call returns a result",
			payload: `{"jsonrpc":"2.0","method":"echo","params":{"value":"hi"},"id":1}`,
			want:    `{"jsonrpc":"2.0","result":{"value":"hi"},"id":1}`,
		},
		{
			desc:    "unknown method returns method not found",
			payload: `{"jsonrpc":"2.0","method":"missing","id":1}`,
			want:    `{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found","data":"missing"},"id":1}`,
		},
		{
			desc:    "handler error is mapped to a server code",
			payload: `{"jsonrpc":"2.0","method":"boom","id":"x"}`,
			want:    `{"jsonrpc":"2.0","error":{"code":-32001,"message":"not found"},"id":"x"}`,
		},
		{
			desc:    "malformed json returns a parse error with a null id",
			payload: `{"jsonrpc":"2.0",`,
			want:    `{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error"},"id":null}`,
		},
		{
			desc:    "wrong version returns invalid request",
			payload: `{"jsonrpc":"1.0","method":"echo","id":1}`,
			want:    `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request","data":"jsonrpc version must be 2.0"},"id":1}`,
		},
		{
			desc:     "notification produces no response",
			payload:  `{"jsonrpc":"2.0","method":"echo","params":{"value":"hi"}}`,
			wantNone: true,
		},
		{
			desc:     "notification to an unknown method produces no response",
			payload:  `{"jsonrpc":"2.0","method":"missing"}`,
			wantNone: true,
		},
		{
			desc:    "a success response always carries a result member",
			payload: `{"jsonrpc":"2.0","method":"noop","id":9}`,
			want:    `{"jsonrpc":"2.0","result":null,"id":9}`,
		},
		{
			desc:    "an invalid id type is reported without leaking internals",
			payload: `{"jsonrpc":"2.0","method":"echo","id":true}`,
			want:    `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request","data":"id must be a string, a number, or null"},"id":null}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			out := testServer().Handle(context.Background(), []byte(tc.payload))
			if tc.wantNone {
				assert.Nil(t, out)

				return
			}
			require.NotNil(t, out)
			assert.JSONEq(t, tc.want, string(out))
		})
	}
}

func TestServerHandleBatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc     string
		payload  string
		want     string
		wantNone bool
	}{
		{
			desc:    "batch returns one response per request",
			payload: `[{"jsonrpc":"2.0","method":"echo","params":{"value":"a"},"id":1},{"jsonrpc":"2.0","method":"echo","params":{"value":"b"},"id":2}]`,
			want:    `[{"jsonrpc":"2.0","result":{"value":"a"},"id":1},{"jsonrpc":"2.0","result":{"value":"b"},"id":2}]`,
		},
		{
			desc:    "notifications are omitted from a mixed batch",
			payload: `[{"jsonrpc":"2.0","method":"echo","params":{"value":"a"}},{"jsonrpc":"2.0","method":"echo","params":{"value":"b"},"id":2}]`,
			want:    `[{"jsonrpc":"2.0","result":{"value":"b"},"id":2}]`,
		},
		{
			desc:     "a batch of only notifications produces no response",
			payload:  `[{"jsonrpc":"2.0","method":"echo","params":{"value":"a"}},{"jsonrpc":"2.0","method":"noop"}]`,
			wantNone: true,
		},
		{
			desc:    "an empty batch is an invalid request",
			payload: `[]`,
			want:    `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request","data":"batch must not be empty"},"id":null}`,
		},
		{
			desc:    "invalid members are reported individually",
			payload: `[1,{"jsonrpc":"2.0","method":"echo","params":{"value":"a"},"id":2}]`,
			want:    `[{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request","data":"request must be a JSON object"},"id":null},{"jsonrpc":"2.0","result":{"value":"a"},"id":2}]`,
		},
		{
			desc:    "errors and results coexist in one batch",
			payload: `[{"jsonrpc":"2.0","method":"boom","id":1},{"jsonrpc":"2.0","method":"echo","params":{"value":"a"},"id":2}]`,
			want:    `[{"jsonrpc":"2.0","error":{"code":-32001,"message":"not found"},"id":1},{"jsonrpc":"2.0","result":{"value":"a"},"id":2}]`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			out := testServer().Handle(context.Background(), []byte(tc.payload))
			if tc.wantNone {
				assert.Nil(t, out)

				return
			}
			require.NotNil(t, out)
			assert.JSONEq(t, tc.want, string(out))
		})
	}
}

func TestServerNotificationStillInvokesHandler(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := jsonrpc.NewServer()
	srv.Register("count", func(_ context.Context, _ json.RawMessage) (any, error) {
		calls.Add(1)

		var empty any

		return empty, nil
	})

	out := srv.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","method":"count"}`))
	assert.Nil(t, out)
	assert.Equal(t, int64(1), calls.Load())
}

func TestServerPassesContext(t *testing.T) {
	t.Parallel()

	type ctxKey string
	key := ctxKey("tenant")

	srv := jsonrpc.NewServer()
	srv.Register("tenant", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return ctx.Value(key), nil
	})

	ctx := context.WithValue(context.Background(), key, "acme")
	out := srv.Handle(ctx, []byte(`{"jsonrpc":"2.0","method":"tenant","id":1}`))
	assert.JSONEq(t, `{"jsonrpc":"2.0","result":"acme","id":1}`, string(out))
}

func TestServerMethods(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"boom", "echo", "noop"}, testServer().Methods())
}
