package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/storage/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPropletListByAlive(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	since := now.Add(-proplet.AliveTimeout)

	cases := []struct {
		desc          string
		proplets      []proplet.Proplet
		offset        uint64
		limit         uint64
		alive         bool
		expectedTotal uint64
		expectedLen   int
	}{
		{
			desc: "list active proplets",
			proplets: []proplet.Proplet{
				{
					ID:           uuid.NewString(),
					Name:         "active-a",
					AliveHistory: []time.Time{now.Add(-2 * time.Second)},
				},
				{
					ID:           uuid.NewString(),
					Name:         "active-b",
					AliveHistory: []time.Time{now.Add(-1 * time.Second)},
				},
				{
					ID:           uuid.NewString(),
					Name:         "inactive-a",
					AliveHistory: []time.Time{now.Add(-proplet.AliveTimeout - time.Second)},
				},
				{
					ID:   uuid.NewString(),
					Name: "inactive-b",
				},
			},
			offset:        0,
			limit:         10,
			alive:         true,
			expectedTotal: 2,
			expectedLen:   2,
		},
		{
			desc: "list inactive proplets",
			proplets: []proplet.Proplet{
				{
					ID:           uuid.NewString(),
					Name:         "active-a",
					AliveHistory: []time.Time{now.Add(-2 * time.Second)},
				},
				{
					ID:           uuid.NewString(),
					Name:         "active-b",
					AliveHistory: []time.Time{now.Add(-1 * time.Second)},
				},
				{
					ID:           uuid.NewString(),
					Name:         "inactive-a",
					AliveHistory: []time.Time{now.Add(-proplet.AliveTimeout - time.Second)},
				},
				{
					ID:   uuid.NewString(),
					Name: "inactive-b",
				},
			},
			offset:        0,
			limit:         10,
			alive:         false,
			expectedTotal: 2,
			expectedLen:   2,
		},
		{
			desc: "paginate active proplets",
			proplets: []proplet.Proplet{
				{
					ID:           uuid.NewString(),
					Name:         uuid.NewString(),
					AliveHistory: []time.Time{now},
				},
				{
					ID:           uuid.NewString(),
					Name:         uuid.NewString(),
					AliveHistory: []time.Time{now},
				},
				{
					ID:           uuid.NewString(),
					Name:         uuid.NewString(),
					AliveHistory: []time.Time{now},
				},
				{
					ID:           uuid.NewString(),
					Name:         uuid.NewString(),
					AliveHistory: []time.Time{now},
				},
				{
					ID:           uuid.NewString(),
					Name:         uuid.NewString(),
					AliveHistory: []time.Time{now},
				},
			},
			offset:        1,
			limit:         2,
			alive:         true,
			expectedTotal: 5,
			expectedLen:   2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			repo := sqlite.NewPropletRepository(newTestDB(t))
			ctx := context.Background()

			for _, p := range tc.proplets {
				require.NoError(t, repo.Create(ctx, p))
			}

			page, total, err := repo.ListByAlive(ctx, tc.offset, tc.limit, tc.alive, since)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedTotal, total)
			assert.Len(t, page, tc.expectedLen)
		})
	}
}
