package worker

import (
	"context"

	"github.com/go-kit/kit/endpoint"
)

func DeployAppEndpoint(s Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(DeployAppRequest)
		err := s.DeployApp(ctx, req.AppName)
		if err != nil {
			return DeployAppResponse{Err: err.Error()}, nil
		}
		return DeployAppResponse{}, nil
	}
}

func StopAppEndpoint(s Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(StopAppRequest)
		err := s.StopApp(ctx, req.AppName)
		if err != nil {
			return StopAppResponse{Err: err.Error()}, nil
		}
		return StopAppResponse{}, nil
	}
}

func PublishDiscoveryEndpoint(s Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		err := s.PublishDiscovery(ctx)
		if err != nil {
			return PublishDiscoveryResponse{Err: err.Error()}, nil
		}
		return PublishDiscoveryResponse{}, nil
	}
}

func ListenForAppChunksEndpoint(s Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(ListenForAppChunksRequest)
		err := s.ListenForAppChunks(ctx, req.AppName)
		if err != nil {
			return ListenForAppChunksResponse{Err: err.Error()}, nil
		}
		return ListenForAppChunksResponse{}, nil
	}
}

func SendTelemetryEndpoint(s Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		err := s.SendTelemetry(ctx)
		if err != nil {
			return SendTelemetryResponse{Err: err.Error()}, nil
		}
		return SendTelemetryResponse{}, nil
	}
}

func HandleRPCCommandEndpoint(s Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(HandleRPCCommandRequest)
		err := s.HandleRPCCommand(ctx, req.Command, req.Params)
		if err != nil {
			return HandleRPCCommandResponse{Err: err.Error()}, nil
		}
		return HandleRPCCommandResponse{}, nil
	}
}
