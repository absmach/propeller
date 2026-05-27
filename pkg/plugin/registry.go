package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type staticRegistry struct {
	plugins []Plugin
	logger  *slog.Logger
}

func LoadDirectory(ctx context.Context, dir string, logger *slog.Logger) (Registry, error) {
	r := &staticRegistry{logger: logger}

	if dir == "" {
		return r, nil
	}

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.WarnContext(ctx, "plugin directory does not exist", "dir", dir)

			return r, nil
		}

		return nil, fmt.Errorf("stat plugin dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("plugin path is not a directory: %s", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read plugin dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".wasm") {
			continue
		}

		path := filepath.Join(dir, e.Name())
		name := strings.TrimSuffix(e.Name(), ".wasm")

		p, err := LoadWasm(ctx, name, path, logger)
		if err != nil {
			logger.ErrorContext(ctx, "failed to load plugin", "path", path, "error", err)

			continue
		}

		logger.InfoContext(ctx, "loaded plugin", "name", name, "path", path)
		r.plugins = append(r.plugins, p)
	}

	return r, nil
}

func (r *staticRegistry) List() []Plugin {
	out := make([]Plugin, len(r.plugins))
	copy(out, r.plugins)

	return out
}

func (r *staticRegistry) Close(ctx context.Context) error {
	var errs []error
	for _, p := range r.plugins {
		if err := p.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
