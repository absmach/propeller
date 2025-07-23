package storage

import (
	"context"
	"sync"

	"github.com/absmach/propeller/pkg/errors"
)

type inMemoryStorage struct {
	sync.Mutex

	data map[string]interface{}
}

func NewInMemoryStorage() Storage {
	return &inMemoryStorage{
		data: make(map[string]interface{}),
	}
}

func (s *inMemoryStorage) Create(_ context.Context, key string, value interface{}) error {
	if key == "" {
		return errors.ErrEmptyKey
	}

	s.Lock()
	defer s.Unlock()

	if _, ok := s.data[key]; ok {
		return errors.ErrEntityExists
	}

	s.data[key] = value

	return nil
}

func (s *inMemoryStorage) Get(_ context.Context, key string) (interface{}, error) {
	if key == "" {
		return nil, errors.ErrEmptyKey
	}

	s.Lock()
	defer s.Unlock()

	if val, ok := s.data[key]; ok {
		return val, nil
	}

	return nil, errors.ErrNotFound
}

func (s *inMemoryStorage) Update(_ context.Context, key string, value interface{}) error {
	if key == "" {
		return errors.ErrEmptyKey
	}

	s.Lock()
	defer s.Unlock()

	if _, ok := s.data[key]; !ok {
		return errors.ErrNotFound
	}

	s.data[key] = value

	return nil
}

func (s *inMemoryStorage) List(_ context.Context, offset, limit uint64) (result []interface{}, total uint64, err error) {
	s.Lock()
	defer s.Unlock()

	keys := make([]string, 0)
	for k := range s.data {
		keys = append(keys, k)
	}

	total = uint64(len(keys))
	if offset >= total {
		return nil, 0, nil
	}

	end := offset + limit
	if end > total {
		end = total
	}

	result = make([]interface{}, end-offset)
	for i := offset; i < end; i++ {
		result[i-offset] = s.data[keys[i]]
	}

	return result, total, nil
}

func (s *inMemoryStorage) Delete(_ context.Context, key string) error {
	if key == "" {
		return errors.ErrEmptyKey
	}

	s.Lock()
	defer s.Unlock()

	delete(s.data, key)

	return nil
}
