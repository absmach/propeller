package proplet

import "context"

var ResultsTopic = "m/%s/c/%s/control/proplet/results"

type Runtime interface {
	StartApp(ctx context.Context, wasmBinary []byte, cliArgs []string, id, functionName string, daemon bool, env map[string]string, args ...uint64) error
	StopApp(ctx context.Context, id string) error
}
