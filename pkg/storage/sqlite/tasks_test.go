package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/absmach/propeller/pkg/storage/sqlite"
	"github.com/absmach/propeller/pkg/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskCreate_WithWasmHTTPURL(t *testing.T) {
	t.Parallel()
	repo := sqlite.NewTaskRepository(newTestDB(t))

	url := "http://example.com/module.wasm"
	tk := task.Task{
		ID:          uuid.NewString(),
		Name:        "http-wasm-task",
		State:       task.Pending,
		WasmHTTPURL: url,
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
	}

	created, err := repo.Create(context.Background(), tk)
	require.NoError(t, err)
	assert.Equal(t, tk.ID, created.ID)
	assert.Equal(t, url, created.WasmHTTPURL)

	fetched, err := repo.Get(context.Background(), tk.ID)
	require.NoError(t, err)
	assert.Equal(t, url, fetched.WasmHTTPURL)
}

func TestTaskCreate_WasmHTTPURLNull(t *testing.T) {
	t.Parallel()
	repo := sqlite.NewTaskRepository(newTestDB(t))

	tk := task.Task{
		ID:        uuid.NewString(),
		Name:      "no-http-url-task",
		State:     task.Pending,
		ImageURL:  "oci://example.com/image:latest",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}

	created, err := repo.Create(context.Background(), tk)
	require.NoError(t, err)
	assert.Empty(t, created.WasmHTTPURL)

	fetched, err := repo.Get(context.Background(), tk.ID)
	require.NoError(t, err)
	assert.Empty(t, fetched.WasmHTTPURL)
}
