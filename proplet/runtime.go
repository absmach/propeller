package proplet

import (
	"context"
	"fmt"
	"sync"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// WazeroRuntime manages the Wazero runtime and running Wasm modules.
type WazeroRuntime struct {
	runtime wazero.Runtime
	modules map[string]api.Module
	mutex   sync.Mutex
}

// NewWazeroRuntime initializes a new WazeroRuntime instance.
func NewWazeroRuntime(ctx context.Context) *WazeroRuntime {
	return &WazeroRuntime{
		runtime: wazero.NewRuntime(ctx),
		modules: make(map[string]api.Module),
	}
}

// StartApp instantiates and starts a Wasm module.
func (w *WazeroRuntime) StartApp(ctx context.Context, appName string, wasmBinary []byte, functionName string) (api.Function, error) {
	if appName == "" {
		return nil, fmt.Errorf("start app: appName is required but missing: %w", pkgerrors.ErrMissingValue)
	}
	if len(wasmBinary) == 0 {
		return nil, fmt.Errorf("start app: Wasm binary is empty: %w", pkgerrors.ErrInvalidValue)
	}
	if functionName == "" {
		return nil, fmt.Errorf("start app: functionName is required but missing: %w", pkgerrors.ErrMissingValue)
	}

	w.mutex.Lock()
	defer w.mutex.Unlock()

	if _, exists := w.modules[appName]; exists {
		return nil, fmt.Errorf("start app: app '%s' is already running: %w", appName, pkgerrors.ErrAppAlreadyRunning)
	}

	module, err := w.runtime.Instantiate(ctx, wasmBinary)
	if err != nil {
		return nil, fmt.Errorf("start app: failed to instantiate Wasm module for app '%s': %w", appName, pkgerrors.ErrModuleInstantiation)
	}

	function := module.ExportedFunction(functionName)
	if function == nil {
		_ = module.Close(ctx)
		return nil, fmt.Errorf("start app: function '%s' not found in Wasm module for app '%s': %w", functionName, appName, pkgerrors.ErrFunctionNotFound)
	}

	w.modules[appName] = module
	return function, nil
}

// StopApp stops and removes a running Wasm module.
func (w *WazeroRuntime) StopApp(ctx context.Context, appName string) error {
	if appName == "" {
		return fmt.Errorf("stop app: appName is required but missing: %w", pkgerrors.ErrMissingValue)
	}

	w.mutex.Lock()
	defer w.mutex.Unlock()

	module, exists := w.modules[appName]
	if !exists {
		return fmt.Errorf("stop app: app '%s' is not running: %w", appName, pkgerrors.ErrAppNotRunning)
	}

	if err := module.Close(ctx); err != nil {
		return fmt.Errorf("stop app: failed to stop app '%s': %w", appName, pkgerrors.ErrModuleStopFailed)
	}

	delete(w.modules, appName)
	return nil
}
