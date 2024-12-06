package worker

import (
	"context"
)

type Service interface {
	DeployApp(ctx context.Context, appName string) error
	StopApp(ctx context.Context, appName string) error
	PublishDiscovery(ctx context.Context) error
	ListenForAppChunks(ctx context.Context, appName string) error
	SendTelemetry(ctx context.Context) error
	HandleRPCCommand(ctx context.Context, command string, params []string) error
}
