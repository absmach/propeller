// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"errors"
	"net/http"
	"testing"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGetTaskErrorStatusCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc       string
		svcErr     error
		wantStatus int
	}{
		{
			desc:       "existing task returns 200",
			wantStatus: http.StatusOK,
		},
		{
			desc:       "missing task returns 404",
			svcErr:     pkgerrors.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			desc:       "conflict returns 409",
			svcErr:     pkgerrors.ErrConflict,
			wantStatus: http.StatusConflict,
		},
		{
			desc:       "invalid value returns 400",
			svcErr:     pkgerrors.ErrInvalidValue,
			wantStatus: http.StatusBadRequest,
		},
		{
			desc:       "unexpected error returns 500",
			svcErr:     errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			ts, svc := newServer(t)
			defer ts.Close()

			id := uuid.NewString()
			svc.On("GetTask", mock.Anything, id).Return(task.Task{ID: id, Name: "task"}, tc.svcErr)

			res, err := http.Get(ts.URL + "/tasks/" + id)
			require.NoError(t, err)
			defer res.Body.Close()

			assert.Equal(t, tc.wantStatus, res.StatusCode)
		})
	}
}

func TestGetTaskInvalidIDReturnsBadRequest(t *testing.T) {
	t.Parallel()

	ts, _ := newServer(t)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/tasks/not-a-uuid")
	require.NoError(t, err)
	defer res.Body.Close()

	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
}
