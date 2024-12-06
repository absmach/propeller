package storage

import "context"

type Storage interface {
	Create(ctx context.Context, key string, value interface{}) error
	Get(ctx context.Context, key string) (interface{}, error)
	Update(ctx context.Context, key string, value interface{}) error
	List(ctx context.Context, offset, limit uint64) ([]interface{}, uint64, error)
	Delete(ctx context.Context, key string) error
}
