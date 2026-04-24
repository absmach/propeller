package manager_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/absmach/propeller/manager"
	mqttmocks "github.com/absmach/propeller/pkg/mqtt/mocks"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newServiceWithRepos(t *testing.T) (manager.Service, *storage.Repositories) {
	t.Helper()
	repos, err := storage.NewRepositories(storage.Config{Type: "memory"})
	require.NoError(t, err)
	sched := scheduler.NewRoundRobin()
	pubsub := mqttmocks.NewMockPubSub(t)
	pubsub.On("Publish", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	pubsub.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	pubsub.On("Unsubscribe", mock.Anything, mock.Anything).Return(nil).Maybe()
	pubsub.On("Disconnect", mock.Anything).Return(nil).Maybe()
	logger := slog.Default()

	svc, _ := manager.NewService(repos, sched, pubsub, "test-domain", "test-channel", logger)

	return svc, repos
}

func TestListPropletsNoFilter(t *testing.T) {
	t.Parallel()
	svc, repos := newServiceWithRepos(t)
	ctx := context.Background()

	// Create two proplets: one alive, one dead.
	err := repos.Proplets.Create(ctx, proplet.Proplet{
		ID:           "active-proplet",
		Name:         "Active",
		AliveHistory: []time.Time{time.Now()},
	})
	require.NoError(t, err)

	err = repos.Proplets.Create(ctx, proplet.Proplet{
		ID:           "inactive-proplet",
		Name:         "Inactive",
		AliveHistory: []time.Time{time.Now().Add(-1 * time.Minute)},
	})
	require.NoError(t, err)

	page, err := svc.ListProplets(ctx, 0, 100, "")
	require.NoError(t, err)
	assert.Equal(t, uint64(2), page.Total)
	assert.Len(t, page.Proplets, 2)
}

func TestListPropletsFilterActive(t *testing.T) {
	t.Parallel()
	svc, repos := newServiceWithRepos(t)
	ctx := context.Background()

	// Active proplet — heartbeat within aliveTimeout (10s).
	err := repos.Proplets.Create(ctx, proplet.Proplet{
		ID:           "active-proplet",
		Name:         "Active",
		AliveHistory: []time.Time{time.Now()},
	})
	require.NoError(t, err)

	// Inactive proplet — heartbeat older than aliveTimeout.
	err = repos.Proplets.Create(ctx, proplet.Proplet{
		ID:           "inactive-proplet",
		Name:         "Inactive",
		AliveHistory: []time.Time{time.Now().Add(-1 * time.Minute)},
	})
	require.NoError(t, err)

	page, err := svc.ListProplets(ctx, 0, 100, "active")
	require.NoError(t, err)
	assert.Equal(t, uint64(1), page.Total)
	require.Len(t, page.Proplets, 1)
	assert.Equal(t, "active-proplet", page.Proplets[0].ID)
	assert.True(t, page.Proplets[0].Alive)
}

func TestListPropletsFilterInactive(t *testing.T) {
	t.Parallel()
	svc, repos := newServiceWithRepos(t)
	ctx := context.Background()

	err := repos.Proplets.Create(ctx, proplet.Proplet{
		ID:           "active-proplet",
		Name:         "Active",
		AliveHistory: []time.Time{time.Now()},
	})
	require.NoError(t, err)

	err = repos.Proplets.Create(ctx, proplet.Proplet{
		ID:           "inactive-proplet",
		Name:         "Inactive",
		AliveHistory: []time.Time{time.Now().Add(-1 * time.Minute)},
	})
	require.NoError(t, err)

	page, err := svc.ListProplets(ctx, 0, 100, "inactive")
	require.NoError(t, err)
	assert.Equal(t, uint64(1), page.Total)
	require.Len(t, page.Proplets, 1)
	assert.Equal(t, "inactive-proplet", page.Proplets[0].ID)
	assert.False(t, page.Proplets[0].Alive)
}

func TestListPropletsFilterPagination(t *testing.T) {
	t.Parallel()
	svc, repos := newServiceWithRepos(t)
	ctx := context.Background()

	// Create 3 active proplets.
	for i := range 3 {
		err := repos.Proplets.Create(ctx, proplet.Proplet{
			ID:           "active-" + string(rune('a'+i)),
			Name:         "Active",
			AliveHistory: []time.Time{time.Now()},
		})
		require.NoError(t, err)
	}

	// Create 1 inactive proplet.
	err := repos.Proplets.Create(ctx, proplet.Proplet{
		ID:           "inactive-x",
		Name:         "Inactive",
		AliveHistory: []time.Time{time.Now().Add(-1 * time.Minute)},
	})
	require.NoError(t, err)

	// Page 1 of active proplets with limit 2.
	page, err := svc.ListProplets(ctx, 0, 2, "active")
	require.NoError(t, err)
	assert.Equal(t, uint64(3), page.Total)
	assert.Len(t, page.Proplets, 2)

	// Page 2 of active proplets.
	page2, err := svc.ListProplets(ctx, 2, 2, "active")
	require.NoError(t, err)
	assert.Equal(t, uint64(3), page2.Total)
	assert.Len(t, page2.Proplets, 1)
}

func TestListPropletsInvalidStatusFilter(t *testing.T) {
	t.Parallel()
	svc, _ := newServiceWithRepos(t)

	_, err := svc.ListProplets(context.Background(), 0, 100, "invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid proplet status filter")
}
