// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package storage_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/absmach/propeller/pkg/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRepositoryNotFoundErrorsAreNormalised(t *testing.T) {
	t.Parallel()

	backends := []struct {
		name string
		cfg  storage.Config
	}{
		{
			name: "badger",
			cfg: storage.Config{
				Type:       "badger",
				BadgerPath: filepath.Join(t.TempDir(), "badger"),
			},
		},
		{
			name: "sqlite",
			cfg: storage.Config{
				Type:       "sqlite",
				SQLitePath: filepath.Join(t.TempDir(), "propeller.db"),
			},
		},
	}

	for _, backend := range backends {
		t.Run(backend.name, func(t *testing.T) {
			t.Parallel()

			repos, err := storage.NewRepositories(backend.cfg)
			require.NoError(t, err)
			if repos.Closer != nil {
				t.Cleanup(func() { _ = repos.Closer.Close() })
			}

			ctx := context.Background()
			missingID := uuid.NewString()

			_, err = repos.Tasks.Get(ctx, missingID)
			require.ErrorIs(t, err, storage.ErrTaskNotFound)

			_, err = repos.Jobs.Get(ctx, missingID)
			require.ErrorIs(t, err, storage.ErrNotFound)

			_, err = repos.TaskProplets.Get(ctx, missingID)
			require.ErrorIs(t, err, storage.ErrNotFound)
		})
	}
}
