//go:build !cgo

package plugin

import (
	"context"
	"errors"
	"log/slog"
)

func LoadWasm(_ context.Context, _, _ string, _ *slog.Logger) (Plugin, error) {
	return nil, errors.New("wasm plugin support requires CGO; rebuild with CGO_ENABLED=1")
}
