package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/absmach/magistrala"
	"github.com/absmach/magistrala/pkg/apiutil"
	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/api"
	"github.com/go-chi/chi/v5"
	kithttp "github.com/go-kit/kit/transport/http"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func MakeHandler(svc manager.Service, logger *slog.Logger, instanceID string) http.Handler {
	mux := chi.NewRouter()

	opts := []kithttp.ServerOption{
		kithttp.ServerErrorEncoder(apiutil.LoggingErrorEncoder(logger, api.EncodeError)),
	}

	mux.Route("/workers", func(r chi.Router) {
		r.Post("/", otelhttp.NewHandler(kithttp.NewServer(
			createWorkerEndpoint(svc),
			decodeCreateWorkerReq,
			api.EncodeResponse,
			opts...,
		), "create-worker").ServeHTTP)
		r.Get("/", otelhttp.NewHandler(kithttp.NewServer(
			listWorkersEndpoint(svc),
			decodeListEntityReq,
			api.EncodeResponse,
			opts...,
		), "list-workers").ServeHTTP)
		r.Route("/{workerID}", func(r chi.Router) {
			r.Get("/", otelhttp.NewHandler(kithttp.NewServer(
				getWorkerEndpoint(svc),
				decodeEntityReq("workerID"),
				api.EncodeResponse,
				opts...,
			), "get-worker").ServeHTTP)
			r.Put("/", otelhttp.NewHandler(kithttp.NewServer(
				updateWorkerEndpoint(svc),
				decodeWorkerTaskReq,
				api.EncodeResponse,
				opts...,
			), "update-worker").ServeHTTP)
			r.Delete("/", otelhttp.NewHandler(kithttp.NewServer(
				deleteWorkerEndpoint(svc),
				decodeEntityReq("workerID"),
				api.EncodeResponse,
				opts...,
			), "delete-worker").ServeHTTP)
		})
	})

	mux.Route("/tasks", func(r chi.Router) {
		r.Post("/", otelhttp.NewHandler(kithttp.NewServer(
			createTaskEndpoint(svc),
			decodeTaskReq,
			api.EncodeResponse,
			opts...,
		), "create-task").ServeHTTP)
		r.Get("/", otelhttp.NewHandler(kithttp.NewServer(
			listTasksEndpoint(svc),
			decodeListEntityReq,
			api.EncodeResponse,
			opts...,
		), "list-tasks").ServeHTTP)
		r.Route("/{taskID}", func(r chi.Router) {
			r.Get("/", otelhttp.NewHandler(kithttp.NewServer(
				getTaskEndpoint(svc),
				decodeEntityReq("taskID"),
				api.EncodeResponse,
				opts...,
			), "get-task").ServeHTTP)
			r.Put("/", otelhttp.NewHandler(kithttp.NewServer(
				updateTaskEndpoint(svc),
				decodeUpdateTaskReq,
				api.EncodeResponse,
				opts...,
			), "update-task").ServeHTTP)
			r.Delete("/", otelhttp.NewHandler(kithttp.NewServer(
				deleteTaskEndpoint(svc),
				decodeEntityReq("taskID"),
				api.EncodeResponse,
				opts...,
			), "delete-task").ServeHTTP)
		})
	})

	mux.Get("/health", magistrala.Health("manager", instanceID))
	mux.Handle("/metrics", promhttp.Handler())

	return mux
}

func decodeCreateWorkerReq(_ context.Context, r *http.Request) (interface{}, error) {
	if !strings.Contains(r.Header.Get("Content-Type"), api.ContentType) {
		return nil, errors.Join(apiutil.ErrValidation, apiutil.ErrUnsupportedContentType)
	}
	var req workerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.Join(err, apiutil.ErrValidation)
	}

	return req, nil
}

func decodeWorkerTaskReq(_ context.Context, r *http.Request) (interface{}, error) {
	if !strings.Contains(r.Header.Get("Content-Type"), api.ContentType) {
		return nil, errors.Join(apiutil.ErrValidation, apiutil.ErrUnsupportedContentType)
	}
	var req workerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.Join(err, apiutil.ErrValidation)
	}
	req.Worker.ID = chi.URLParam(r, "workerID")

	return req, nil
}

func decodeEntityReq(key string) kithttp.DecodeRequestFunc {
	return func(_ context.Context, r *http.Request) (interface{}, error) {
		return entityReq{
			id: chi.URLParam(r, key),
		}, nil
	}
}

func decodeTaskReq(_ context.Context, r *http.Request) (interface{}, error) {
	if !strings.Contains(r.Header.Get("Content-Type"), api.ContentType) {
		return nil, errors.Join(apiutil.ErrValidation, apiutil.ErrUnsupportedContentType)
	}
	var req taskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.Join(err, apiutil.ErrValidation)
	}

	return req, nil
}

func decodeUpdateTaskReq(_ context.Context, r *http.Request) (interface{}, error) {
	if !strings.Contains(r.Header.Get("Content-Type"), api.ContentType) {
		return nil, errors.Join(apiutil.ErrValidation, apiutil.ErrUnsupportedContentType)
	}
	var req taskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.Join(err, apiutil.ErrValidation)
	}
	req.Task.ID = chi.URLParam(r, "taskID")

	return req, nil
}

func decodeListEntityReq(_ context.Context, r *http.Request) (interface{}, error) {
	o, err := apiutil.ReadNumQuery[uint64](r, api.OffsetKey, api.DefOffset)
	if err != nil {
		return nil, errors.Join(apiutil.ErrValidation, err)
	}

	l, err := apiutil.ReadNumQuery[uint64](r, api.LimitKey, api.DefLimit)
	if err != nil {
		return nil, errors.Join(apiutil.ErrValidation, err)
	}

	return listEntityReq{
		offset: o,
		limit:  l,
	}, nil
}