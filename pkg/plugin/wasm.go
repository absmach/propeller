//go:build cgo

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"github.com/bytecodealliance/wasmtime-go/v44"
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
	engine         *wasmtime.Engine
	store          *wasmtime.Store
	instance       *wasmtime.Instance
	alloc          *wasmtime.Func
	free           *wasmtime.Func
	auth           *wasmtime.Func
	enrich         *wasmtime.Func
	beforeSelect   *wasmtime.Func
	beforeDispatch *wasmtime.Func
	onStart        *wasmtime.Func
	onDone         *wasmtime.Func
	mu             sync.Mutex
	stdout         *slogWriter
	stderr         *slogWriter
	stdoutFile     *os.File
	stderrFile     *os.File
	stdoutOffset   int64
	stderrOffset   int64
}

// slogWriter is a line-buffered io.Writer that emits each complete line as a
// slog record. This prevents plugin stdout/stderr from bypassing structured
// logging and makes it clear in logs which plugin produced the output.
type slogWriter struct {
	logger *slog.Logger
	level  slog.Level
	plugin string
	mu     sync.Mutex
	buf    []byte
}

func (w *slogWriter) Write(p []byte) (int, error) {
	return w.writeCtx(context.Background(), p)
}

func (w *slogWriter) Flush(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		line := string(bytes.TrimRight(w.buf, "\r\n"))
		if line != "" {
			w.logger.Log(ctx, w.level, line, "plugin", w.plugin)
		}
		w.buf = w.buf[:0]
	}
}

func (w *slogWriter) writeCtx(ctx context.Context, p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(bytes.TrimRight(w.buf[:idx], "\r"))
		w.buf = w.buf[idx+1:]
		if line != "" {
			w.logger.Log(ctx, w.level, line, "plugin", w.plugin)
		}
	}

	return len(p), nil
}

func LoadWasm(ctx context.Context, name, path string, logger *slog.Logger) (Plugin, error) {
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin %s: %w", path, err)
	}

	cfg := wasmtime.NewConfig()
	cfg.SetEpochInterruption(true)
	engine := wasmtime.NewEngineWithConfig(cfg)

	module, err := wasmtime.NewModule(engine, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("compile plugin %s: %w", name, err)
	}

	stdoutFile, err := os.CreateTemp("", "propeller-plugin-"+name+"-stdout-*")
	if err != nil {
		return nil, fmt.Errorf("create stdout capture for plugin %s: %w", name, err)
	}
	stderrFile, err := os.CreateTemp("", "propeller-plugin-"+name+"-stderr-*")
	if err != nil {
		_ = stdoutFile.Close()
		_ = os.Remove(stdoutFile.Name())

		return nil, fmt.Errorf("create stderr capture for plugin %s: %w", name, err)
	}

	cleanupFiles := func() {
		_ = stdoutFile.Close()
		_ = os.Remove(stdoutFile.Name())
		_ = stderrFile.Close()
		_ = os.Remove(stderrFile.Name())
	}

	wasiCfg := wasmtime.NewWasiConfig()
	if err := wasiCfg.SetStdoutFile(stdoutFile.Name()); err != nil {
		cleanupFiles()

		return nil, fmt.Errorf("configure stdout for plugin %s: %w", name, err)
	}
	if err := wasiCfg.SetStderrFile(stderrFile.Name()); err != nil {
		cleanupFiles()

		return nil, fmt.Errorf("configure stderr for plugin %s: %w", name, err)
	}

	store := wasmtime.NewStore(engine)
	store.SetWasi(wasiCfg)

	linker := wasmtime.NewLinker(engine)
	if err := linker.DefineWasi(); err != nil {
		cleanupFiles()

		return nil, fmt.Errorf("define wasi for plugin %s: %w", name, err)
	}

	instance, err := linker.Instantiate(store, module)
	if err != nil {
		cleanupFiles()

		return nil, fmt.Errorf("instantiate plugin %s: %w", name, err)
	}

	p := &wasmPlugin{
		name:           name,
		engine:         engine,
		store:          store,
		instance:       instance,
		alloc:          instance.GetFunc(store, exportAlloc),
		free:           instance.GetFunc(store, exportFree),
		auth:           instance.GetFunc(store, exportAuthorize),
		enrich:         instance.GetFunc(store, exportEnrich),
		beforeSelect:   instance.GetFunc(store, exportOnBeforeSelect),
		beforeDispatch: instance.GetFunc(store, exportOnBeforeDispatch),
		onStart:        instance.GetFunc(store, exportOnStart),
		onDone:         instance.GetFunc(store, exportOnComplete),
		stdout:         &slogWriter{logger: logger, level: slog.LevelInfo, plugin: name},
		stderr:         &slogWriter{logger: logger, level: slog.LevelWarn, plugin: name},
		stdoutFile:     stdoutFile,
		stderrFile:     stderrFile,
	}

	if p.alloc == nil || p.free == nil {
		_ = p.stdoutFile.Close()
		_ = p.stderrFile.Close()
		_ = os.Remove(stdoutFile.Name())
		_ = os.Remove(stderrFile.Name())

		return nil, fmt.Errorf("plugin %s missing required exports: %s, %s", name, exportAlloc, exportFree)
	}

	if p.auth == nil {
		logger.WarnContext(ctx, "plugin loaded without authorize export — all requests will be permitted", "plugin", name)
	}

	return p, nil
}

func (p *wasmPlugin) Name() string { return p.name }

func (p *wasmPlugin) Close(ctx context.Context) error {
	p.stdout.Flush(ctx)
	p.stderr.Flush(ctx)

	_ = p.stdoutFile.Close()
	_ = p.stderrFile.Close()
	_ = os.Remove(p.stdoutFile.Name())
	_ = os.Remove(p.stderrFile.Name())

	return nil
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

func (p *wasmPlugin) invoke(ctx context.Context, fn *wasmtime.Func, input, output any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal plugin input: %w", err)
	}

	// Reset epoch deadline before any wasm call so a prior context cancellation
	// (which increments the epoch) doesn't cause alloc to trap immediately.
	p.store.SetEpochDeadline(1)

	inPtr, err := p.writeBuffer(data)
	if err != nil {
		return err
	}
	defer p.freeBuffer(inPtr, uint32(len(data)))
	stop := make(chan struct{})
	goroutineDone := make(chan struct{})
	go func() {
		defer close(goroutineDone)
		select {
		case <-ctx.Done():
			p.engine.IncrementEpoch()
		case <-stop:
		}
	}()

	result, callErr := fn.Call(p.store, int32(inPtr), int32(len(data)))
	close(stop)
	<-goroutineDone

	p.drainOutput(ctx)

	if callErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return fmt.Errorf("plugin %s call: %w", p.name, callErr)
	}

	if output == nil {
		return nil
	}

	if result == nil {
		return fmt.Errorf("plugin %s returned nil, expected i64", p.name)
	}

	v, ok := result.(int64)
	if !ok {
		return fmt.Errorf("plugin %s returned unexpected result type", p.name)
	}
	packed := uint64(v)
	outPtr, outLen := unpack(packed)
	if outLen == 0 {
		return nil
	}
	defer p.freeBuffer(outPtr, outLen)

	mem := p.instance.GetExport(p.store, "memory").Memory()
	raw := mem.UnsafeData(p.store)
	if uint64(outPtr)+uint64(outLen) > uint64(len(raw)) {
		runtime.KeepAlive(mem)

		return fmt.Errorf("plugin %s response pointer out of memory bounds (ptr=%d len=%d memsize=%d)", p.name, outPtr, outLen, len(raw))
	}
	out := make([]byte, outLen)
	copy(out, raw[outPtr:outPtr+outLen])
	runtime.KeepAlive(mem)

	if err := json.Unmarshal(out, output); err != nil {
		return fmt.Errorf("unmarshal plugin output: %w", err)
	}

	return nil
}

func (p *wasmPlugin) writeBuffer(data []byte) (uint32, error) {
	result, err := p.alloc.Call(p.store, int32(len(data)))
	if err != nil {
		return 0, fmt.Errorf("plugin alloc: %w", err)
	}
	if result == nil {
		return 0, errors.New("plugin alloc returned nil")
	}

	v, ok := result.(int32)
	if !ok {
		return 0, errors.New("plugin alloc returned unexpected result type")
	}
	ptr := uint32(v)
	if ptr == 0 {
		return 0, errors.New("plugin alloc returned null pointer")
	}

	mem := p.instance.GetExport(p.store, "memory").Memory()
	raw := mem.UnsafeData(p.store)
	if uint64(ptr)+uint64(len(data)) > uint64(len(raw)) {
		runtime.KeepAlive(mem)

		return 0, fmt.Errorf("plugin alloc returned pointer out of memory bounds (ptr=%d len=%d memsize=%d)", ptr, len(data), len(raw))
	}
	copy(raw[ptr:ptr+uint32(len(data))], data)
	runtime.KeepAlive(mem)

	return ptr, nil
}

func (p *wasmPlugin) freeBuffer(ptr, length uint32) {
	if ptr == 0 {
		return
	}
	_, _ = p.free.Call(p.store, int32(ptr), int32(length))
}

func (p *wasmPlugin) drainOutput(ctx context.Context) {
	p.drainFile(ctx, p.stdoutFile, &p.stdoutOffset, p.stdout)
	p.stdout.Flush(ctx)
	p.drainFile(ctx, p.stderrFile, &p.stderrOffset, p.stderr)
	p.stderr.Flush(ctx)
}

func (p *wasmPlugin) drainFile(ctx context.Context, f *os.File, offset *int64, w *slogWriter) {
	if _, err := f.Seek(*offset, io.SeekStart); err != nil {
		return
	}
	buf := make([]byte, 4096)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			_, _ = w.writeCtx(ctx, buf[:n])
			*offset += int64(n)
		}
		if readErr != nil {
			break
		}
	}
}

func unpack(v uint64) (ptr, length uint32) {
	return uint32(v >> 32), uint32(v)
}
