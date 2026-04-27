package badger

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/absmach/propeller/pkg/proplet"
)

type propletRepo struct {
	db *Database
}

func NewPropletRepository(db *Database) PropletRepository {
	return &propletRepo{db: db}
}

func (r *propletRepo) Create(ctx context.Context, p proplet.Proplet) error {
	key := []byte("proplet:" + p.ID)
	val, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}
	if err := r.db.set(key, val); err != nil {
		return fmt.Errorf("%w: %w", ErrCreate, err)
	}

	return nil
}

func (r *propletRepo) Get(ctx context.Context, id string) (proplet.Proplet, error) {
	key := []byte("proplet:" + id)
	val, err := r.db.get(key)
	if err != nil {
		return proplet.Proplet{}, ErrPropletNotFound
	}
	var p proplet.Proplet
	if err := json.Unmarshal(val, &p); err != nil {
		return proplet.Proplet{}, fmt.Errorf("unmarshal error: %w", err)
	}

	return p, nil
}

func (r *propletRepo) Update(ctx context.Context, p proplet.Proplet) error {
	key := []byte("proplet:" + p.ID)
	val, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}
	if err := r.db.set(key, val); err != nil {
		return fmt.Errorf("%w: %w", ErrUpdate, err)
	}

	return nil
}

func (r *propletRepo) List(ctx context.Context, offset, limit uint64) ([]proplet.Proplet, uint64, error) {
	prefix := []byte("proplet:")
	total, err := r.db.countWithPrefix(prefix)
	if err != nil {
		return nil, 0, err
	}
	values, err := r.db.listWithPrefix(prefix, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	proplets := make([]proplet.Proplet, len(values))
	for i, val := range values {
		var p proplet.Proplet
		if err := json.Unmarshal(val, &p); err != nil {
			return nil, 0, fmt.Errorf("unmarshal error: %w", err)
		}
		proplets[i] = p
	}

	return proplets, total, nil
}

const maxBadgerScan uint64 = 100000

func (r *propletRepo) ListByAlive(ctx context.Context, offset, limit uint64, alive bool, since time.Time) ([]proplet.Proplet, uint64, error) {
	prefix := []byte("proplet:")
	total, err := r.db.countWithPrefix(prefix)
	if err != nil {
		return nil, 0, err
	}
	scanLimit := total
	if total > maxBadgerScan {
		// Badger has no index for liveness filtering; a full keyspace scan is
		// required. Beyond maxBadgerScan records the scan is truncated and results
		// will be incomplete. TODO: consider an indexed approach for large deployments.
		slog.Warn("Badger ListByAlive scan truncated", "total", total, "cap", maxBadgerScan)
		scanLimit = maxBadgerScan
	}

	// Badger cannot push liveness filtering into key iteration, so we scan the
	// full proplet keyspace up to scanLimit and paginate the filtered slice.
	values, err := r.db.listWithPrefix(prefix, 0, scanLimit)
	if err != nil {
		return nil, 0, err
	}

	var filtered []proplet.Proplet
	for _, val := range values {
		var p proplet.Proplet
		if err := json.Unmarshal(val, &p); err != nil {
			return nil, 0, fmt.Errorf("unmarshal error: %w", err)
		}
		isAlive := len(p.AliveHistory) > 0 && !p.AliveHistory[len(p.AliveHistory)-1].Before(since)
		if isAlive == alive {
			filtered = append(filtered, p)
		}
	}

	filteredTotal := uint64(len(filtered))
	if offset >= filteredTotal {
		return []proplet.Proplet{}, filteredTotal, nil
	}
	end := min(offset+limit, filteredTotal)

	return filtered[offset:end], filteredTotal, nil
}

func (r *propletRepo) Delete(ctx context.Context, id string) error {
	key := []byte("proplet:" + id)

	return r.db.delete(key)
}

func (r *propletRepo) GetAliveHistory(ctx context.Context, id string, offset, limit uint64) ([]time.Time, uint64, error) {
	p, err := r.Get(ctx, id)
	if err != nil {
		return nil, 0, err
	}

	total := uint64(len(p.AliveHistory))
	if offset >= total {
		return []time.Time{}, total, nil
	}

	end := min(offset+limit, total)

	return p.AliveHistory[offset:end], total, nil
}
