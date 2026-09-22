package sdk_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/absmach/propeller/pkg/jsonrpc"
	"github.com/absmach/propeller/pkg/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRPCTestSDK(t *testing.T, handler http.HandlerFunc) sdk.SDK {
	t.Helper()

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	return sdk.NewSDK(sdk.Config{ManagerURL: ts.URL})
}

func TestCall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc     string
		method   string
		params   any
		response string
		want     string
		wantErr  bool
		errCode  int
	}{
		{
			desc:     "a result is returned raw",
			method:   jsonrpc.MethodPropletList,
			response: `{"jsonrpc":"2.0","result":{"total":2},"id":1}`,
			want:     `{"total":2}`,
		},
		{
			desc:     "params are forwarded",
			method:   jsonrpc.MethodTaskStart,
			params:   map[string]string{"id": "abc"},
			response: `{"jsonrpc":"2.0","result":{"ok":true},"id":1}`,
			want:     `{"ok":true}`,
		},
		{
			desc:     "an error response becomes an error",
			method:   jsonrpc.MethodTaskGet,
			params:   map[string]string{"id": "missing"},
			response: `{"jsonrpc":"2.0","error":{"code":-32001,"message":"not found"},"id":1}`,
			wantErr:  true,
			errCode:  jsonrpc.CodeNotFound,
		},
		{
			desc:     "an unknown method becomes an error",
			method:   "task.explode",
			response: `{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":1}`,
			wantErr:  true,
			errCode:  jsonrpc.CodeMethodNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			var captured jsonrpc.Request
			psdk := newRPCTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					return
				}
				assert.NoError(t, json.Unmarshal(body, &captured))

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			})

			result, err := psdk.Call(tc.method, tc.params)
			if tc.wantErr {
				require.Error(t, err)

				var rpcErr *jsonrpc.Error
				require.ErrorAs(t, err, &rpcErr)
				assert.Equal(t, tc.errCode, rpcErr.Code)

				return
			}

			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(result))
			assert.Equal(t, jsonrpc.Version, captured.JSONRPC)
			assert.Equal(t, tc.method, captured.Method)
			require.NotNil(t, captured.ID)
		})
	}
}

func TestCallBatch(t *testing.T) {
	t.Parallel()

	psdk := newRPCTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			return
		}

		var requests []jsonrpc.Request
		assert.NoError(t, json.Unmarshal(body, &requests))
		assert.Len(t, requests, 2)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			`[{"jsonrpc":"2.0","result":{"ok":true},"id":1},` +
				`{"jsonrpc":"2.0","error":{"code":-32001,"message":"not found"},"id":2}]`,
		))
	})

	responses, err := psdk.CallBatch([]sdk.RPCCall{
		{Method: jsonrpc.MethodTaskStart, Params: map[string]string{"id": "abc"}},
		{Method: jsonrpc.MethodTaskGet, Params: map[string]string{"id": "missing"}},
	})
	require.NoError(t, err)
	require.Len(t, responses, 2)

	assert.Nil(t, responses[0].Error)
	require.NotNil(t, responses[1].Error)
	assert.Equal(t, jsonrpc.CodeNotFound, responses[1].Error.Code)
}

func TestCallBatchRejectsEmpty(t *testing.T) {
	t.Parallel()

	psdk := newRPCTestSDK(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("server should not be called for an empty batch")
	})

	_, err := psdk.CallBatch(nil)
	require.Error(t, err)
}

func TestCallUsesUniqueIDs(t *testing.T) {
	t.Parallel()

	ids := make(chan string, 2)
	psdk := newRPCTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			return
		}

		var req jsonrpc.Request
		assert.NoError(t, json.Unmarshal(body, &req))
		ids <- req.ID.String()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":null,"id":1}`))
	})

	_, err := psdk.Call(jsonrpc.MethodPropletList, nil)
	require.NoError(t, err)
	_, err = psdk.Call(jsonrpc.MethodPropletList, nil)
	require.NoError(t, err)

	first, second := <-ids, <-ids
	assert.NotEqual(t, first, second)
}
