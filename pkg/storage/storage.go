package storage

import "context"

// Storage is a generic key-value storage interface used internally by storage adapters.
// It is an implementation detail and should not be used directly outside of the storage package.
// Use the typed repository interfaces (TaskRepository, PropletRepository, etc.) instead.
//
// For memory storage implementations, note that List operations may have performance limitations
// due to in-memory filtering. See memory_adapter.go for details.
type Storage interface {
	Create(ctx context.Context, key string, value any) error
	Get(ctx context.Context, key string) (any, error)
	Update(ctx context.Context, key string, value any) error
	List(ctx context.Context, offset, limit uint64) ([]any, uint64, error)
	Delete(ctx context.Context, key string) error
}
