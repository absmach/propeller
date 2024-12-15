package proplet

import (
	"context"
	"fmt"
	"sync"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/tetratelabs/wazero"
	wazeroapi "github.com/tetratelabs/wazero/api"
)

type Runtime interface {
	StartApp(ctx context.Context, appName string, wasmBinary []byte, functionName string) (wazeroapi.Function, error)
	StopApp(ctx context.Context, appName string) error
}

type WazeroRuntime struct {
	runtimes map[string]wazero.Runtime
	modules  map[string]wazeroapi.Module
	mutex    sync.Mutex
}

var _ Runtime = (*WazeroRuntime)(nil)

func (w *WazeroRuntime) StartApp(ctx context.Context, appName string, wasmBinary []byte, functionName string) (wazeroapi.Function, error) {
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

	runtime := wazero.NewRuntime(ctx)

	module, err := runtime.Instantiate(ctx, wasmBinary)
	if err != nil {
		runtime.Close(ctx)

		return nil, fmt.Errorf("start app: failed to instantiate Wasm module for app '%s': %w", appName, pkgerrors.ErrModuleInstantiation)
	}

	function := module.ExportedFunction(functionName)
	if function == nil {
		module.Close(ctx)
		runtime.Close(ctx)

		return nil, fmt.Errorf("start app: function '%s' not found in Wasm module for app '%s': %w", functionName, appName, pkgerrors.ErrFunctionNotFound)
	}

	w.modules[appName] = module
	if w.runtimes == nil {
		w.runtimes = make(map[string]wazero.Runtime)
	}
	w.runtimes[appName] = runtime

	return function, nil
}

func (w *WazeroRuntime) StopApp(ctx context.Context, appName string) error {
	if appName == "" {
		return fmt.Errorf("stop app: appName is required but missing: %w", pkgerrors.ErrMissingValue)
	}

	w.mutex.Lock()
	defer w.mutex.Unlock()

	module, moduleExists := w.modules[appName]
	runtime, runtimeExists := w.runtimes[appName]

	if !moduleExists || !runtimeExists {
		return fmt.Errorf("stop app: app '%s' is not running: %w", appName, pkgerrors.ErrAppNotRunning)
	}

	if err := module.Close(ctx); err != nil {
		return fmt.Errorf("stop app: failed to close module for app '%s': %w", appName, err)
	}
	if err := runtime.Close(ctx); err != nil {
		return fmt.Errorf("stop app: failed to close runtime for app '%s': %w", appName, err)
	}

	delete(w.runtimes, appName)
	delete(w.modules, appName)

	return nil
}
