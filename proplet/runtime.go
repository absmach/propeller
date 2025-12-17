package proplet

import "context"

var ResultsTopic = "m/%s/c/%s/control/proplet/results"

type StartConfig struct {
	ID           string
	FunctionName string
	Daemon       bool
	WasmBinary   []byte
	CLIArgs      []string
	Env          map[string]string
	Args         []uint64
}

type Runtime interface {
	StartApp(ctx context.Context, config StartConfig) error
	StopApp(ctx context.Context, id string) error
}
