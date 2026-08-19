// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package jsonrpc_test

import (
	"errors"
	"fmt"
	"testing"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/jsonrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorFrom(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc string
		err  error
		code int
	}{
		{
			desc: "not found maps to the not found code",
			err:  pkgerrors.ErrNotFound,
			code: jsonrpc.CodeNotFound,
		},
		{
			desc: "wrapped not found maps to the not found code",
			err:  fmt.Errorf("looking up task: %w", pkgerrors.ErrNotFound),
			code: jsonrpc.CodeNotFound,
		},
		{
			desc: "conflict maps to the conflict code",
			err:  pkgerrors.ErrConflict,
			code: jsonrpc.CodeConflict,
		},
		{
			desc: "entity exists maps to the conflict code",
			err:  pkgerrors.ErrEntityExists,
			code: jsonrpc.CodeConflict,
		},
		{
			desc: "invalid data maps to invalid params",
			err:  pkgerrors.ErrInvalidData,
			code: jsonrpc.CodeInvalidParams,
		},
		{
			desc: "invalid value maps to validation",
			err:  pkgerrors.ErrInvalidValue,
			code: jsonrpc.CodeValidation,
		},
		{
			desc: "missing value maps to validation",
			err:  pkgerrors.ErrMissingValue,
			code: jsonrpc.CodeValidation,
		},
		{
			desc: "invalid method maps to method not found",
			err:  pkgerrors.ErrInvalidMethod,
			code: jsonrpc.CodeMethodNotFound,
		},
		{
			desc: "timeout maps to the timeout code",
			err:  pkgerrors.ErrTimeout,
			code: jsonrpc.CodeTimeout,
		},
		{
			desc: "unknown errors map to internal error",
			err:  errors.New("boom"),
			code: jsonrpc.CodeInternalError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			rpcErr := jsonrpc.ErrorFrom(tc.err)
			require.NotNil(t, rpcErr)
			assert.Equal(t, tc.code, rpcErr.Code)
			assert.Equal(t, tc.err.Error(), rpcErr.Message)
		})
	}
}

func TestErrorFromNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, jsonrpc.ErrorFrom(nil))
}

func TestErrorFromPreservesRPCError(t *testing.T) {
	t.Parallel()

	original := jsonrpc.InvalidParams("inputs must be an array")
	wrapped := fmt.Errorf("dispatching: %w", original)

	assert.Same(t, original, jsonrpc.ErrorFrom(wrapped))
}

func TestErrorImplementsError(t *testing.T) {
	t.Parallel()

	err := jsonrpc.NewError(jsonrpc.CodeInternalError, "boom", nil)
	assert.Equal(t, "jsonrpc error -32603: boom", err.Error())
}

func TestConstructorCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc string
		err  *jsonrpc.Error
		code int
	}{
		{desc: "parse error", err: jsonrpc.ParseError(nil), code: jsonrpc.CodeParseError},
		{desc: "invalid request", err: jsonrpc.InvalidRequest(nil), code: jsonrpc.CodeInvalidRequest},
		{desc: "method not found", err: jsonrpc.MethodNotFound("m"), code: jsonrpc.CodeMethodNotFound},
		{desc: "invalid params", err: jsonrpc.InvalidParams(nil), code: jsonrpc.CodeInvalidParams},
		{desc: "internal error", err: jsonrpc.InternalError(nil), code: jsonrpc.CodeInternalError},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.code, tc.err.Code)
			assert.NotEmpty(t, tc.err.Message)
		})
	}
}
