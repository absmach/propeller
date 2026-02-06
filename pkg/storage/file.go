package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"

	"github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
)

const (
	defaultDataDir = "./data"
)

type fileStorage struct {
	sync.RWMutex

	dataDir string
}

type storedValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

func NewFileStorage(dataDir string) (Storage, error) {
	if dataDir == "" {
		dataDir = defaultDataDir
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	return &fileStorage{
		dataDir: dataDir,
	}, nil
}

func (s *fileStorage) Create(ctx context.Context, key string, value any) error {
	if key == "" {
		return errors.ErrEmptyKey
	}

	s.Lock()
	defer s.Unlock()

	filePath := s.filePath(key)

	if _, err := os.Stat(filePath); err == nil {
		return errors.ErrEntityExists
	}

	return s.writeFile(ctx, filePath, value)
}

func (s *fileStorage) Get(ctx context.Context, key string) (any, error) {
	if key == "" {
		return nil, errors.ErrEmptyKey
	}

	s.RLock()
	defer s.RUnlock()

	filePath := s.filePath(key)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.ErrNotFound
		}

		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var stored storedValue
	if err := json.Unmarshal(data, &stored); err == nil && stored.Type != "" {
		return s.unmarshalByType(stored.Type, stored.Value)
	}

	return s.unmarshalByDetection(data)
}

func (s *fileStorage) Update(ctx context.Context, key string, value any) error {
	if key == "" {
		return errors.ErrEmptyKey
	}

	s.Lock()
	defer s.Unlock()

	filePath := s.filePath(key)

	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNotFound
		}

		return fmt.Errorf("failed to check file: %w", err)
	}

	return s.writeFile(ctx, filePath, value)
}

func (s *fileStorage) List(ctx context.Context, offset, limit uint64) (result []any, total uint64, err error) {
	s.RLock()
	defer s.RUnlock()

	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	total = uint64(len(files))
	if offset >= total {
		return nil, total, nil
	}

	end := min(offset+limit, total)

	result = make([]any, 0, end-offset)
	for i := offset; i < end; i++ {
		key := files[i][:len(files[i])-5]
		filePath := s.filePath(key)

		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var stored storedValue
		if err := json.Unmarshal(data, &stored); err == nil && stored.Type != "" {
			value, err := s.unmarshalByType(stored.Type, stored.Value)
			if err == nil {
				result = append(result, value)

				continue
			}
		}

		value, err := s.unmarshalByDetection(data)
		if err == nil {
			result = append(result, value)
		}
	}

	return result, total, nil
}

func (s *fileStorage) Delete(ctx context.Context, key string) error {
	if key == "" {
		return errors.ErrEmptyKey
	}

	s.Lock()
	defer s.Unlock()

	filePath := s.filePath(key)

	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNotFound
		}

		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

func (s *fileStorage) filePath(key string) string {
	return filepath.Join(s.dataDir, key+".json")
}

func (s *fileStorage) writeFile(ctx context.Context, filePath string, value any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	typeName := s.getTypeName(value)

	valueData, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	stored := storedValue{
		Type:  typeName,
		Value: valueData,
	}

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	return nil
}

func (s *fileStorage) getTypeName(value any) string {
	if value == nil {
		return "nil"
	}

	t := reflect.TypeOf(value)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	return t.String()
}

func (s *fileStorage) unmarshalByType(typeName string, data json.RawMessage) (any, error) {
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
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}

		return s, nil
	default:
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}

		return m, nil
	}
}

func (s *fileStorage) unmarshalByDetection(data []byte) (any, error) {
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
