package storage

import "context"

type Storage interface {
	Create(ctx context.Context, key string, value any) error
	Get(ctx context.Context, key string) (any, error)
	Update(ctx context.Context, key string, value any) error
	List(ctx context.Context, offset, limit uint64) ([]any, uint64, error)
	Delete(ctx context.Context, key string) error
}
