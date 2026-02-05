package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
	"github.com/dgraph-io/badger/v4"
)

const (
	defaultBadgerDir = "./data"
)

type storedValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type badgerStorage struct {
	sync.RWMutex

	db *badger.DB
}

func NewBadgerStorage(dataDir string) (Storage, error) {
	if dataDir == "" {
		dataDir = defaultBadgerDir
	}

	if err := ensureDir(dataDir); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "badger.db")
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil // Disable Badger's default logger

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open Badger database: %w", err)
	}

	return &badgerStorage{
		db: db,
	}, nil
}

func (s *badgerStorage) Create(ctx context.Context, key string, value any) error {
	if key == "" {
		return pkgerrors.ErrEmptyKey
	}

	s.Lock()
	defer s.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(key))
		if err == nil {
			return pkgerrors.ErrEntityExists
		}
		if !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("failed to check key existence: %w", err)
		}

		return s.writeValue(txn, key, value)
	})
}

func (s *badgerStorage) Get(ctx context.Context, key string) (any, error) {
	if key == "" {
		return nil, pkgerrors.ErrEmptyKey
	}

	s.RLock()
	defer s.RUnlock()

	var result any
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return pkgerrors.ErrNotFound
			}

			return fmt.Errorf("failed to get key: %w", err)
		}

		return item.Value(func(val []byte) error {
			result, err = s.unmarshalValue(val)

			return err
		})
	})

	return result, err
}

func (s *badgerStorage) Update(ctx context.Context, key string, value any) error {
	if key == "" {
		return pkgerrors.ErrEmptyKey
	}

	s.Lock()
	defer s.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(key))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return pkgerrors.ErrNotFound
			}

			return fmt.Errorf("failed to check key existence: %w", err)
		}

		return s.writeValue(txn, key, value)
	})
}

func (s *badgerStorage) List(ctx context.Context, offset, limit uint64) (result []any, total uint64, err error) {
	s.RLock()
	defer s.RUnlock()

	var keys []string
	err = s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			keys = append(keys, string(it.Item().Key()))
		}

		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list keys: %w", err)
	}

	sort.Strings(keys)
	total = uint64(len(keys))

	if offset >= total {
		return nil, total, nil
	}

	end := min(offset+limit, total)
	result = make([]any, 0, end-offset)

	err = s.db.View(func(txn *badger.Txn) error {
		for i := offset; i < end; i++ {
			item, err := txn.Get([]byte(keys[i]))
			if err != nil {
				continue
			}

			err = item.Value(func(val []byte) error {
				value, err := s.unmarshalValue(val)
				if err == nil {
					result = append(result, value)
				}

				return err
			})
			if err != nil {
				continue
			}
		}

		return nil
	})

	return result, total, err
}

func (s *badgerStorage) Delete(ctx context.Context, key string) error {
	if key == "" {
		return pkgerrors.ErrEmptyKey
	}

	s.Lock()
	defer s.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(key))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return pkgerrors.ErrNotFound
			}

			return fmt.Errorf("failed to check key existence: %w", err)
		}

		return txn.Delete([]byte(key))
	})
}

func (s *badgerStorage) Close() error {
	s.Lock()
	defer s.Unlock()

	return s.db.Close()
}

func (s *badgerStorage) writeValue(txn *badger.Txn, key string, value any) error {
	typeName := s.getTypeName(value)

	valueData, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	stored := storedValue{
		Type:  typeName,
		Value: valueData,
	}

	data, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("failed to marshal stored value: %w", err)
	}

	return txn.Set([]byte(key), data)
}

func (s *badgerStorage) unmarshalValue(data []byte) (any, error) {
	var stored storedValue
	if err := json.Unmarshal(data, &stored); err == nil && stored.Type != "" {
		return s.unmarshalByType(stored.Type, stored.Value)
	}

	return s.unmarshalByDetection(data)
}

func (s *badgerStorage) getTypeName(value any) string {
	if value == nil {
		return "nil"
	}

	t := reflect.TypeOf(value)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	return t.String()
}

func (s *badgerStorage) unmarshalByType(typeName string, data json.RawMessage) (any, error) {
	switch typeName {
	case "task.Task":
		var t task.Task
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, err
		}

		return t, nil
	case "proplet.Proplet":
		var p proplet.Proplet
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}

		return p, nil
	case "string":
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return nil, err
		}

		return str, nil
	default:
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}

		return m, nil
	}
}

func (s *badgerStorage) unmarshalByDetection(data []byte) (any, error) {
	var t task.Task
	if err := json.Unmarshal(data, &t); err == nil {
		if t.ID != "" {
			return t, nil
		}
	}

	var p proplet.Proplet
	if err := json.Unmarshal(data, &p); err == nil {
		if p.ID != "" {
			return p, nil
		}
	}

	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		return str, nil
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	return m, nil
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
