package worker

import (
	"context"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

type WasmRuntime struct {
	runtime wazero.Runtime
	modules map[string]api.Module
	mutex   sync.Mutex
}

func NewWasmRuntime(ctx context.Context) *WasmRuntime {
	return &WasmRuntime{
		runtime: wazero.NewRuntime(ctx),
		modules: make(map[string]api.Module),
	}
}

func (w *WasmRuntime) StartApp(ctx context.Context, appName string, wasmBinary []byte, functionName string) (api.Function, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if _, exists := w.modules[appName]; exists {
		return nil, fmt.Errorf("app %s is already running", appName)
	}

	module, err := w.runtime.Instantiate(ctx, wasmBinary)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate Wasm module for app %s: %v", appName, err)
	}

	function := module.ExportedFunction(functionName)
	if function == nil {
		_ = module.Close(ctx)
		return nil, fmt.Errorf("function %s not found in Wasm module for app %s", functionName, appName)
	}

	w.modules[appName] = module
	return function, nil
}

func (w *WasmRuntime) StopApp(ctx context.Context, appName string) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	module, exists := w.modules[appName]
	if !exists {
		return fmt.Errorf("app %s is not running", appName)
	}

	if err := module.Close(ctx); err != nil {
		return fmt.Errorf("failed to stop app %s: %v", appName, err)
	}

	delete(w.modules, appName)
	return nil
}
