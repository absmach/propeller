package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const (
	exportAlloc            = "plugin_alloc"
	exportFree             = "plugin_free"
	exportAuthorize        = "authorize"
	exportEnrich           = "enrich_task"
	exportOnBeforeSelect   = "on_before_proplet_select"
	exportOnBeforeDispatch = "on_before_dispatch"
	exportOnStart          = "on_task_start"
	exportOnComplete       = "on_task_complete"
)

type wasmPlugin struct {
	name           string
	runtime        wazero.Runtime
	module         api.Module
	alloc          api.Function
	free           api.Function
	auth           api.Function
	enrich         api.Function
	beforeSelect   api.Function
	beforeDispatch api.Function
	onStart        api.Function
	onDone         api.Function
	mu             sync.Mutex
}

func LoadWasm(ctx context.Context, name, path string) (Plugin, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin %s: %w", path, err)
	}

	rt := wazero.NewRuntime(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		_ = rt.Close(ctx)

		return nil, fmt.Errorf("wasi instantiate: %w", err)
	}

	cfg := wazero.NewModuleConfig().
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		WithName(name)

	mod, err := rt.InstantiateWithConfig(ctx, bytes, cfg)
	if err != nil {
		_ = rt.Close(ctx)

		return nil, fmt.Errorf("instantiate plugin %s: %w", name, err)
	}

	p := &wasmPlugin{
		name:           name,
		runtime:        rt,
		module:         mod,
		alloc:          mod.ExportedFunction(exportAlloc),
		free:           mod.ExportedFunction(exportFree),
		auth:           mod.ExportedFunction(exportAuthorize),
		enrich:         mod.ExportedFunction(exportEnrich),
		beforeSelect:   mod.ExportedFunction(exportOnBeforeSelect),
		beforeDispatch: mod.ExportedFunction(exportOnBeforeDispatch),
		onStart:        mod.ExportedFunction(exportOnStart),
		onDone:         mod.ExportedFunction(exportOnComplete),
	}

	if p.alloc == nil || p.free == nil {
		_ = rt.Close(ctx)

		return nil, fmt.Errorf("plugin %s missing required exports: %s, %s", name, exportAlloc, exportFree)
	}

	return p, nil
}

func (p *wasmPlugin) Name() string { return p.name }

func (p *wasmPlugin) Close(ctx context.Context) error {
	return p.runtime.Close(ctx)
}

func (p *wasmPlugin) Authorize(ctx context.Context, req AuthorizeRequest) (AuthorizeResponse, error) {
	if p.auth == nil {
		return AuthorizeResponse{Allow: true}, nil
	}

	var resp AuthorizeResponse
	if err := p.invoke(ctx, p.auth, req, &resp); err != nil {
		return AuthorizeResponse{}, err
	}

	return resp, nil
}

func (p *wasmPlugin) Enrich(ctx context.Context, req EnrichRequest) (EnrichResponse, error) {
	if p.enrich == nil {
		return EnrichResponse{}, nil
	}

	var resp EnrichResponse
	if err := p.invoke(ctx, p.enrich, req, &resp); err != nil {
		return EnrichResponse{}, err
	}

	return resp, nil
}

func (p *wasmPlugin) OnBeforePropletSelect(ctx context.Context, req PropletSelectRequest) (PropletSelectResponse, error) {
	if p.beforeSelect == nil {
		return PropletSelectResponse{Allow: true}, nil
	}

	var resp PropletSelectResponse
	if err := p.invoke(ctx, p.beforeSelect, req, &resp); err != nil {
		return PropletSelectResponse{}, err
	}

	return resp, nil
}

func (p *wasmPlugin) OnBeforeDispatch(ctx context.Context, req DispatchRequest) (DispatchResponse, error) {
	if p.beforeDispatch == nil {
		return DispatchResponse{Allow: true}, nil
	}

	var resp DispatchResponse
	if err := p.invoke(ctx, p.beforeDispatch, req, &resp); err != nil {
		return DispatchResponse{}, err
	}

	return resp, nil
}

func (p *wasmPlugin) OnTaskStart(ctx context.Context, evt TaskEvent) error {
	if p.onStart == nil {
		return nil
	}

	return p.invoke(ctx, p.onStart, evt, nil)
}

func (p *wasmPlugin) OnTaskComplete(ctx context.Context, evt TaskEvent) error {
	if p.onDone == nil {
		return nil
	}

	return p.invoke(ctx, p.onDone, evt, nil)
}

func (p *wasmPlugin) invoke(ctx context.Context, fn api.Function, input, output any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal plugin input: %w", err)
	}

	inPtr, err := p.writeBuffer(ctx, data)
	if err != nil {
		return err
	}
	defer p.freeBuffer(ctx, inPtr, uint32(len(data)))

	results, err := fn.Call(ctx, uint64(inPtr), uint64(len(data)))
	if err != nil {
		return fmt.Errorf("plugin %s call: %w", p.name, err)
	}

	if output == nil {
		return nil
	}

	if len(results) != 1 {
		return fmt.Errorf("plugin %s returned %d values, expected 1", p.name, len(results))
	}

	outPtr, outLen := unpack(results[0])
	if outLen == 0 {
		return nil
	}
	defer p.freeBuffer(ctx, outPtr, outLen)

	bytes, ok := p.module.Memory().Read(outPtr, outLen)
	if !ok {
		return errors.New("plugin memory read failed")
	}

	if err := json.Unmarshal(bytes, output); err != nil {
		return fmt.Errorf("unmarshal plugin output: %w", err)
	}

	return nil
}

func (p *wasmPlugin) writeBuffer(ctx context.Context, data []byte) (uint32, error) {
	results, err := p.alloc.Call(ctx, uint64(len(data)))
	if err != nil {
		return 0, fmt.Errorf("plugin alloc: %w", err)
	}
	if len(results) != 1 {
		return 0, errors.New("plugin alloc returned wrong arity")
	}

	ptr := uint32(results[0])
	if !p.module.Memory().Write(ptr, data) {
		return 0, errors.New("plugin memory write failed")
	}

	return ptr, nil
}

func (p *wasmPlugin) freeBuffer(ctx context.Context, ptr, length uint32) {
	if ptr == 0 {
		return
	}
	_, _ = p.free.Call(ctx, uint64(ptr), uint64(length))
}

func unpack(v uint64) (ptr, length uint32) {
	return uint32(v >> 32), uint32(v)
}
