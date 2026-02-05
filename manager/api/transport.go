package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/api"
	"github.com/absmach/supermq"
	apiutil "github.com/absmach/supermq/api/http/util"
	"github.com/go-chi/chi/v5"
	kithttp "github.com/go-kit/kit/transport/http"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	maxFileSize = 1024 * 1024 * 100
	fileKey     = "file"
)

func MakeHandler(svc manager.Service, logger *slog.Logger, instanceID string) http.Handler {
	mux := chi.NewRouter()

	opts := []kithttp.ServerOption{
		kithttp.ServerErrorEncoder(apiutil.LoggingErrorEncoder(logger, api.EncodeError)),
	}

	mux.Route("/proplets", func(r chi.Router) {
		r.Get("/", otelhttp.NewHandler(kithttp.NewServer(
			listPropletsEndpoint(svc),
			decodeListEntityReq,
			api.EncodeResponse,
			opts...,
		), "list-proplets").ServeHTTP)
		r.Route("/{propletID}", func(r chi.Router) {
			r.Get("/", otelhttp.NewHandler(kithttp.NewServer(
				getPropletEndpoint(svc),
				decodeEntityReq("propletID"),
				api.EncodeResponse,
				opts...,
			), "get-proplet").ServeHTTP)
			r.Delete("/", otelhttp.NewHandler(kithttp.NewServer(
				deletePropletEndpoint(svc),
				decodeEntityReq("propletID"),
				api.EncodeResponse,
				opts...,
			), "delete-proplet").ServeHTTP)
			r.Get("/metrics", otelhttp.NewHandler(kithttp.NewServer(
				getPropletMetricsEndpoint(svc),
				decodeMetricsReq("propletID"),
				api.EncodeResponse,
				opts...,
			), "get-proplet-metrics").ServeHTTP)
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
			r.Put("/upload", otelhttp.NewHandler(kithttp.NewServer(
				updateTaskEndpoint(svc),
				decodeUploadTaskFileReq,
				api.EncodeResponse,
				opts...,
			), "upload-task-file").ServeHTTP)
			r.Delete("/", otelhttp.NewHandler(kithttp.NewServer(
				deleteTaskEndpoint(svc),
				decodeEntityReq("taskID"),
				api.EncodeResponse,
				opts...,
			), "delete-task").ServeHTTP)
			r.Post("/start", otelhttp.NewHandler(kithttp.NewServer(
				startTaskEndpoint(svc),
				decodeEntityReq("taskID"),
				api.EncodeResponse,
				opts...,
			), "start-task").ServeHTTP)
			r.Post("/stop", otelhttp.NewHandler(kithttp.NewServer(
				stopTaskEndpoint(svc),
				decodeEntityReq("taskID"),
				api.EncodeResponse,
				opts...,
			), "stop-task").ServeHTTP)
			r.Get("/metrics", otelhttp.NewHandler(kithttp.NewServer(
				getTaskMetricsEndpoint(svc),
				decodeMetricsReq("taskID"),
				api.EncodeResponse,
				opts...,
			), "get-task-metrics").ServeHTTP)
			r.Get("/results", otelhttp.NewHandler(kithttp.NewServer(
				getTaskResultsEndpoint(svc),
				decodeEntityReq("taskID"),
				api.EncodeResponse,
				opts...,
			), "get-task-results").ServeHTTP)
		})
	})

	// Federated Learning API
	// Manager acts as Orchestrator/Experiment Config (Step 1 in diagram)
	// Also provides forwarding endpoints for compatibility
	mux.Route("/fl", func(r chi.Router) {
		// POST /experiments - Configure experiment with FL Coordinator (Step 1: Orchestrator → Coordinator)
		r.Post("/experiments", otelhttp.NewHandler(kithttp.NewServer(
			configureExperimentEndpoint(svc),
			decodeExperimentConfigReq,
			api.EncodeResponse,
			opts...,
		), "configure-experiment").ServeHTTP)

		// GET /task - Forward task request to FL Coordinator
		//
		// IMPORTANT: Clients MUST call FL Coordinator directly (Step 3 in workflow diagram)
		// This endpoint exists ONLY for compatibility/MQTT forwarding scenarios
		//
		// Correct client flow: Client → FL Coordinator (GET /task)
		// NOT: Client → Manager → FL Coordinator
		r.Get("/task", otelhttp.NewHandler(kithttp.NewServer(
			getFLTaskEndpoint(svc),
			decodeFLTaskReq,
			api.EncodeResponse,
			opts...,
		), "get-fl-task").ServeHTTP)

		// POST /update - Forward update to FL Coordinator
		//
		// IMPORTANT: Clients MUST call FL Coordinator directly (Step 7 in workflow diagram)
		// This endpoint exists ONLY for compatibility/MQTT forwarding scenarios
		// Manager does NOT aggregate - only forwards
		//
		// Correct client flow: Client → FL Coordinator (POST /update)
		// NOT: Client → Manager → FL Coordinator
		r.Post("/update", otelhttp.NewHandler(kithttp.NewServer(
			postFLUpdateEndpoint(svc),
			decodeFLUpdateReq,
			api.EncodeResponse,
			opts...,
		), "post-fl-update").ServeHTTP)

		// POST /update_cbor - Forward CBOR update to FL Coordinator
		//
		// IMPORTANT: Clients MUST call FL Coordinator directly (Step 7 in workflow diagram)
		// This endpoint exists ONLY for compatibility/MQTT forwarding scenarios
		//
		// Correct client flow: Client → FL Coordinator (POST /update_cbor)
		// NOT: Client → Manager → FL Coordinator
		r.Post("/update_cbor", otelhttp.NewHandler(kithttp.NewServer(
			postFLUpdateCBOREndpoint(svc),
			decodeFLUpdateCBORReq,
			api.EncodeResponse,
			opts...,
		), "post-fl-update-cbor").ServeHTTP)

		// GET /rounds/{round_id}/complete - Forward round status request to FL Coordinator
		r.Get("/rounds/{round_id}/complete", otelhttp.NewHandler(kithttp.NewServer(
			getRoundStatusEndpoint(svc),
			decodeRoundStatusReq,
			api.EncodeResponse,
			opts...,
		), "get-round-status").ServeHTTP)
	})

	mux.Post("/workflows", otelhttp.NewHandler(kithttp.NewServer(
		createWorkflowEndpoint(svc),
		decodeWorkflowReq,
		api.EncodeResponse,
		opts...,
	), "create-workflow").ServeHTTP)

	mux.Route("/jobs", func(r chi.Router) {
		r.Post("/", otelhttp.NewHandler(kithttp.NewServer(
			createJobEndpoint(svc),
			decodeJobReq,
			api.EncodeResponse,
			opts...,
		), "create-job").ServeHTTP)
		r.Get("/", otelhttp.NewHandler(kithttp.NewServer(
			listJobsEndpoint(svc),
			decodeListEntityReq,
			api.EncodeResponse,
			opts...,
		), "list-jobs").ServeHTTP)
		r.Route("/{jobID}", func(r chi.Router) {
			r.Get("/", otelhttp.NewHandler(kithttp.NewServer(
				getJobEndpoint(svc),
				decodeEntityReq("jobID"),
				api.EncodeResponse,
				opts...,
			), "get-job").ServeHTTP)
			r.Post("/start", otelhttp.NewHandler(kithttp.NewServer(
				startJobEndpoint(svc),
				decodeEntityReq("jobID"),
				api.EncodeResponse,
				opts...,
			), "start-job").ServeHTTP)
			r.Post("/stop", otelhttp.NewHandler(kithttp.NewServer(
				stopJobEndpoint(svc),
				decodeEntityReq("jobID"),
				api.EncodeResponse,
				opts...,
			), "stop-job").ServeHTTP)
		})
	})

	mux.Get("/health", supermq.Health("manager", instanceID))
	mux.Handle("/metrics", promhttp.Handler())

	return mux
}

func decodeEntityReq(key string) kithttp.DecodeRequestFunc {
	return func(_ context.Context, r *http.Request) (any, error) {
		return entityReq{
			id: chi.URLParam(r, key),
		}, nil
	}
}

func decodeTaskReq(_ context.Context, r *http.Request) (any, error) {
	if !strings.Contains(r.Header.Get("Content-Type"), api.ContentType) {
		return nil, errors.Join(apiutil.ErrValidation, apiutil.ErrUnsupportedContentType)
	}

	var req taskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.Join(err, apiutil.ErrValidation)
	}

	return req, nil
}

func decodeUploadTaskFileReq(_ context.Context, r *http.Request) (any, error) {
	var req taskReq
	if err := r.ParseMultipartForm(maxFileSize); err != nil {
		return nil, err
	}
	file, header, err := r.FormFile(fileKey)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if !strings.HasSuffix(header.Filename, ".wasm") {
		return nil, errors.Join(apiutil.ErrValidation, errors.New("invalid file extension"))
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	req.File = data
	req.ID = chi.URLParam(r, "taskID")

	return req, nil
}

func decodeUpdateTaskReq(_ context.Context, r *http.Request) (any, error) {
	if !strings.Contains(r.Header.Get("Content-Type"), api.ContentType) {
		return nil, errors.Join(apiutil.ErrValidation, apiutil.ErrUnsupportedContentType)
	}
	var req taskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.Join(err, apiutil.ErrValidation)
	}
	req.ID = chi.URLParam(r, "taskID")

	return req, nil
}

func decodeListEntityReq(_ context.Context, r *http.Request) (any, error) {
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

func decodeMetricsReq(key string) kithttp.DecodeRequestFunc {
	return func(_ context.Context, r *http.Request) (any, error) {
		o, err := apiutil.ReadNumQuery[uint64](r, api.OffsetKey, api.DefOffset)
		if err != nil {
			return nil, errors.Join(apiutil.ErrValidation, err)
		}

		l, err := apiutil.ReadNumQuery[uint64](r, api.LimitKey, api.DefLimit)
		if err != nil {
			return nil, errors.Join(apiutil.ErrValidation, err)
		}

		return metricsReq{
			id:     chi.URLParam(r, key),
			offset: o,
			limit:  l,
		}, nil
	}
}

func decodeWorkflowReq(_ context.Context, r *http.Request) (any, error) {
	if !strings.Contains(r.Header.Get("Content-Type"), api.ContentType) {
		return nil, errors.Join(apiutil.ErrValidation, apiutil.ErrUnsupportedContentType)
	}

	var req workflowReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.Join(err, apiutil.ErrValidation)
	}

	return req, nil
}

func decodeJobReq(_ context.Context, r *http.Request) (any, error) {
	if !strings.Contains(r.Header.Get("Content-Type"), api.ContentType) {
		return nil, errors.Join(apiutil.ErrValidation, apiutil.ErrUnsupportedContentType)
	}

	var req jobReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.Join(err, apiutil.ErrValidation)
	}

	return req, nil
}
