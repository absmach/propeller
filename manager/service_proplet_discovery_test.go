// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package manager //nolint:testpackage // needs the unexported MQTT message handler

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/stretchr/testify/require"
)

// Badger surfaces storage.ErrPropletNotFound while the in-memory repos
// normalise to pkgerrors.ErrNotFound, so test against badger.
func newHandlerService(t *testing.T) (*service, storage.PropletRepository) {
	t.Helper()

	repos, err := storage.NewRepositories(storage.Config{
		Type:       "badger",
		BadgerPath: filepath.Join(t.TempDir(), "badger"),
	})
	require.NoError(t, err)
	if repos.Closer != nil {
		t.Cleanup(func() { _ = repos.Closer.Close() })
	}

	svc, _, _ := NewService(repos, scheduler.NewRoundRobin(), nil, "tenant", "channel", "", slog.Default(), nil)

	handler, ok := svc.(*service)
	require.True(t, ok, "expected *service")

	return handler, repos.Proplets
}

func discoveryMsg(id, hostname, version string) map[string]any {
	return map[string]any{
		"proplet_id": id,
		"metadata": map[string]any{
			"hostname":        hostname,
			"proplet_version": version,
			"wasm_runtime":    "wasmtime",
			"ip":              "172.30.0.8",
		},
	}
}

func TestCreatePropletHandlerRefreshesExistingProplet(t *testing.T) {
	t.Parallel()

	svc, repo := newHandlerService(t)
	ctx := context.Background()
	const id = "proplet-1"

	require.NoError(t, svc.createPropletHandler(ctx, map[string]any{"proplet_id": id}))

	before, err := repo.Get(ctx, id)
	require.NoError(t, err)
	require.Empty(t, before.Metadata.Hostname)

	require.NoError(t, svc.createPropletHandler(ctx, discoveryMsg(id, "node-a", "0.6.3")))

	after, err := repo.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "node-a", after.Metadata.Hostname)
	require.Equal(t, "0.6.3", after.Metadata.PropletVersion)
	require.Equal(t, "wasmtime", after.Metadata.WasmRuntime)
	require.Equal(t, "172.30.0.8", after.Metadata.IP)
}

func TestCreatePropletHandlerCreatesNewProplet(t *testing.T) {
	t.Parallel()

	svc, repo := newHandlerService(t)
	ctx := context.Background()
	const id = "proplet-new"

	require.NoError(t, svc.createPropletHandler(ctx, discoveryMsg(id, "node-b", "0.6.3")))

	got, err := repo.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, id, got.ID)
	require.Equal(t, "node-b", got.Metadata.Hostname)
	require.NotEmpty(t, got.Name)
}

func TestCreatePropletHandlerPreservesIdentityOnRefresh(t *testing.T) {
	t.Parallel()

	svc, repo := newHandlerService(t)
	ctx := context.Background()
	const id = "proplet-2"

	require.NoError(t, svc.createPropletHandler(ctx, discoveryMsg(id, "node-c", "0.6.3")))

	original, err := repo.Get(ctx, id)
	require.NoError(t, err)

	require.NoError(t, svc.createPropletHandler(ctx, discoveryMsg(id, "node-c", "0.6.3")))

	refreshed, err := repo.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, original.Name, refreshed.Name)
	require.Equal(t, "0.6.3", refreshed.Metadata.PropletVersion)
}

func TestCreatePropletHandlerRejectsBadID(t *testing.T) {
	t.Parallel()

	svc, _ := newHandlerService(t)
	ctx := context.Background()

	require.Error(t, svc.createPropletHandler(ctx, map[string]any{}))
	require.Error(t, svc.createPropletHandler(ctx, map[string]any{"proplet_id": ""}))
}
