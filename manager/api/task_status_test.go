// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/absmach/propeller/manager"
	managerapi "github.com/absmach/propeller/manager/api"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskNotFoundStatusCodes(t *testing.T) {
	t.Parallel()

	ts := newPersistentServer(t)
	missingID := uuid.NewString()
	cases := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/tasks/" + missingID},
		{method: http.MethodGet, path: "/tasks/" + missingID + "/results"},
		{method: http.MethodPut, path: "/tasks/" + missingID, body: "{}"},
		{method: http.MethodDelete, path: "/tasks/" + missingID},
		{method: http.MethodPost, path: "/tasks/" + missingID + "/start"},
		{method: http.MethodPost, path: "/tasks/" + missingID + "/stop"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()

			assertStatus(t, ts.URL+tc.path, tc.method, tc.body, http.StatusNotFound)
		})
	}
}

func TestJobNotFoundStatusCodes(t *testing.T) {
	t.Parallel()

	ts := newPersistentServer(t)
	missingID := uuid.NewString()

	cases := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/jobs/" + missingID},
		{method: http.MethodPost, path: "/jobs/" + missingID + "/start"},
		{method: http.MethodPost, path: "/jobs/" + missingID + "/stop"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()

			assertStatus(t, ts.URL+tc.path, tc.method, "", http.StatusNotFound)
		})
	}
}

func newPersistentServer(t *testing.T) *httptest.Server {
	t.Helper()

	repos, err := storage.NewRepositories(storage.Config{
		Type:       "badger",
		BadgerPath: filepath.Join(t.TempDir(), "badger"),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if repos.Closer != nil {
			_ = repos.Closer.Close()
		}
	})

	svc, _, _ := manager.NewService(repos, scheduler.NewRoundRobin(), nil, "tenant", "channel", "", slog.Default(), nil)
	ts := httptest.NewServer(managerapi.MakeHandler(svc, slog.Default(), "test"))
	t.Cleanup(ts.Close)

	return ts
}

func assertStatus(t *testing.T, url, method, body string, want int) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	require.NoError(t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()

	assert.Equal(t, want, res.StatusCode)
}
