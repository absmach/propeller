// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package manager_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/absmach/propeller/manager"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTaskNotFoundReturnsDomainError(t *testing.T) {
	t.Parallel()

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
	ctx := context.Background()
	missingID := uuid.NewString()

	_, err = svc.GetTask(ctx, missingID)
	require.ErrorIs(t, err, pkgerrors.ErrNotFound)

	_, err = svc.GetTaskResults(ctx, missingID)
	require.ErrorIs(t, err, pkgerrors.ErrNotFound)

	err = svc.DeleteTask(ctx, missingID)
	require.ErrorIs(t, err, pkgerrors.ErrNotFound)

	created, err := svc.CreateTask(ctx, task.Task{Name: "delete-me"})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteTask(ctx, created.ID))

	_, err = svc.GetTask(ctx, created.ID)
	require.ErrorIs(t, err, pkgerrors.ErrNotFound)
}
