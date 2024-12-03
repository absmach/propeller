package api

import (
	"context"

	"github.com/absmach/propeller/worker"
	"github.com/go-kit/kit/endpoint"
)

func MakeGetWorkerStatEndpoint(svc worker.WorkerService) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(WorkerRequestDTO)
		return svc.GetWorkerStats(ctx, req.WorkerID)
	}
}
