package proplet

import (
	"context"
)

var ResultsTopic = "channels/%s/messages/control/proplet/results"

type Runtime interface {
	StartApp(ctx context.Context, wasmBinary []byte, cliArgs []string, id, functionName string, args ...uint64) error
	StopApp(ctx context.Context, id string) error
}
