package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/absmach/propeller/worker"

	"github.com/go-chi/chi/v5"
	kithttp "github.com/go-kit/kit/transport/http"
)

func MakeHandler(svc worker.WorkerService) http.Handler {
	opts := []kithttp.ServerOption{}

	mux := chi.NewRouter()

	mux.Route("/workers", func(r chi.Router) {
		r.Get("/{worker_id}", kithttp.NewServer(
			MakeGetWorkerStatEndpoint(svc),
			decodeWorkerRequest,
			encodeWorkerResponse,
			opts...,
		).ServeHTTP)
	})

	return mux
}

func decodeWorkerRequest(_ context.Context, r *http.Request) (interface{}, error) {
	if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		return nil, errors.New("unsupported content type")
	}

	workerID := chi.URLParam(r, "worker_id")
	if workerID == "" {
		return nil, errors.New("worker_id is required")
	}

	return WorkerRequestDTO{WorkerID: workerID}, nil
}

func encodeWorkerResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(response)
}
