package manager_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/absmach/propeller/manager"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	mqttmocks "github.com/absmach/propeller/pkg/mqtt/mocks"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/google/uuid"
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
	svc, _, _ := manager.NewService(repos, sched, pubsub, "test-domain", "test-channel", "", logger, nil)

	return svc, repos
}

func TestListPropletsFilterByStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc          string
		status        string
		expectedTotal uint64
		expectedAlive bool
		checkAlive    bool
		err           bool
	}{
		{
			desc:          "list all proplets without filter",
			status:        "",
			expectedTotal: 2,
		},
		{
			desc:          "list active proplets",
			status:        proplet.ActiveStatus.String(),
			expectedTotal: 1,
			expectedAlive: true,
			checkAlive:    true,
		},
		{
			desc:          "list inactive proplets",
			status:        proplet.InactiveStatus.String(),
			expectedTotal: 1,
			expectedAlive: false,
			checkAlive:    true,
		},
		{
			desc:   "invalid status returns error",
			status: "unknown",
			err:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc, repos := newServiceWithRepos(t)
			ctx := context.Background()

			if !tc.err {
				activeProplet := proplet.Proplet{
					ID:           uuid.NewString(),
					Name:         "active-proplet",
					AliveHistory: []time.Time{time.Now()},
				}
				inactiveProplet := proplet.Proplet{
					ID:           uuid.NewString(),
					Name:         "inactive-proplet",
					AliveHistory: []time.Time{time.Now().Add(-1 * time.Hour)},
				}
				require.NoError(t, repos.Proplets.Create(ctx, activeProplet))
				require.NoError(t, repos.Proplets.Create(ctx, inactiveProplet))
			}

			page, err := svc.ListProplets(ctx, 0, 100, tc.status)
			if tc.err {
				require.Error(t, err)
				require.ErrorIs(t, err, pkgerrors.ErrInvalidValue)
				require.ErrorIs(t, err, proplet.ErrInvalidStatus)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedTotal, page.Total)
			if tc.checkAlive && len(page.Proplets) > 0 {
				assert.Equal(t, tc.expectedAlive, page.Proplets[0].Alive)
			}
		})
	}
}

func TestListPropletsFilterPagination(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc          string
		offset        uint64
		limit         uint64
		expectedTotal uint64
		expectedLen   int
	}{
		{
			desc:          "first page",
			offset:        0,
			limit:         3,
			expectedTotal: 5,
			expectedLen:   3,
		},
		{
			desc:          "second page",
			offset:        3,
			limit:         3,
			expectedTotal: 5,
			expectedLen:   2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc, repos := newServiceWithRepos(t)
			ctx := context.Background()

			for range 5 {
				p := proplet.Proplet{
					ID:           uuid.NewString(),
					Name:         uuid.NewString(),
					AliveHistory: []time.Time{time.Now()},
				}
				require.NoError(t, repos.Proplets.Create(ctx, p))
			}

			page, err := svc.ListProplets(ctx, tc.offset, tc.limit, proplet.ActiveStatus.String())
			require.NoError(t, err)
			assert.Equal(t, tc.expectedTotal, page.Total)
			assert.Len(t, page.Proplets, tc.expectedLen)
		})
	}
}
