package worker

import (
	"context"
	"fmt"
	"sync"

	"github.com/absmach/propeller/task"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

var _ Service = (*worker)(nil)

type worker struct {
	mu        sync.Mutex
	Name      string
	DB        map[string]task.Task
	TaskCount int
	runtimes  map[string]wazero.Runtime
	functions map[string]api.Function
}

func NewWasmWorker(name string) *worker {
	return &worker{
		Name:      name,
		DB:        make(map[string]task.Task),
		TaskCount: 0,
		runtimes:  make(map[string]wazero.Runtime),
		functions: make(map[string]api.Function),
	}
}

func (w *worker) StartTask(ctx context.Context, t task.Task) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	r := wazero.NewRuntime(ctx)
	// Instantiate WASI, which implements host functions needed for TinyGo to
	// implement `panic`.
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	module, err := r.Instantiate(ctx, t.Function.File)
	if err != nil {
		return err
	}

	function := module.ExportedFunction(t.Function.Name)
	if function == nil {
		return fmt.Errorf("function %q not found", t.Function.Name)
	}

	w.TaskCount++
	w.runtimes[t.ID] = r
	w.functions[t.ID] = function
	w.DB[t.ID] = t

	return nil
}

func (w *worker) RunTask(ctx context.Context, taskID string) ([]uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	t, ok := w.DB[taskID]
	if !ok {
		return nil, fmt.Errorf("task %q not found", taskID)
	}

	function := w.functions[t.ID]

	result, err := function.Call(ctx, t.Function.Inputs...)
	if err != nil {
		return nil, err
	}

	r := w.runtimes[t.ID]
	if err := r.Close(ctx); err != nil {
		return nil, err
	}

	return result, nil
}

func (w *worker) StopTask(ctx context.Context, taskID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	r := w.runtimes[taskID]

	return r.Close(ctx)
}

func (w *worker) RemoveTask(_ context.Context, taskID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.DB, taskID)
	delete(w.runtimes, taskID)
	delete(w.functions, taskID)
	w.TaskCount--

	return nil
}
