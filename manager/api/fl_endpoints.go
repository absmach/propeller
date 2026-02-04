package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/api"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	apiutil "github.com/absmach/supermq/api/http/util"
	"github.com/go-chi/chi/v5"
	"github.com/go-kit/kit/endpoint"
)

type flTaskReq struct {
	roundID   string
	propletID string
}

type flTaskResponse struct {
	Task manager.FLTask `json:"task"`
}

type flUpdateReq struct {
	Update manager.FLUpdate `json:"update"`
}

type flUpdateResponse struct {
	Status string `json:"status"`
}

type roundStatusReq struct {
	roundID string
}

type roundStatusResponse struct {
	Status manager.RoundStatus `json:"status"`
}

type experimentConfigReq struct {
	Config manager.ExperimentConfig `json:"config"`
}

type experimentConfigResponse struct {
	ExperimentID string `json:"experiment_id"`
	RoundID      string `json:"round_id"`
	Status       string `json:"status"`
}

func configureExperimentEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(experimentConfigReq)
		if !ok {
			return experimentConfigResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}

		if err := svc.ConfigureExperiment(ctx, req.Config); err != nil {
			return experimentConfigResponse{}, err
		}

		return experimentConfigResponse{
			ExperimentID: req.Config.ExperimentID,
			RoundID:      req.Config.RoundID,
			Status:       "configured",
		}, nil
	}
}

func getFLTaskEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(flTaskReq)
		if !ok {
			return flTaskResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}

		task, err := svc.GetFLTask(ctx, req.roundID, req.propletID)
		if err != nil {
			return flTaskResponse{}, err
		}

		return flTaskResponse{Task: task}, nil
	}
}

func postFLUpdateEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(flUpdateReq)
		if !ok {
			return flUpdateResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}

		if err := svc.PostFLUpdate(ctx, req.Update); err != nil {
			return flUpdateResponse{}, err
		}

		return flUpdateResponse{Status: "accepted"}, nil
	}
}

func postFLUpdateCBOREndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.([]byte)
		if !ok {
			return flUpdateResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}

		if err := svc.PostFLUpdateCBOR(ctx, req); err != nil {
			return flUpdateResponse{}, err
		}

		return flUpdateResponse{Status: "accepted"}, nil
	}
}

func getRoundStatusEndpoint(svc manager.Service) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(roundStatusReq)
		if !ok {
			return roundStatusResponse{}, errors.Join(apiutil.ErrValidation, pkgerrors.ErrInvalidData)
		}

		status, err := svc.GetRoundStatus(ctx, req.roundID)
		if err != nil {
			return roundStatusResponse{}, err
		}

		return roundStatusResponse{Status: status}, nil
	}
}

func decodeFLTaskReq(_ context.Context, r *http.Request) (any, error) {
	roundID := r.URL.Query().Get("round_id")
	propletID := r.URL.Query().Get("proplet_id")

	if roundID == "" {
		return nil, errors.Join(apiutil.ErrValidation, errors.New("round_id is required"))
	}

	return flTaskReq{
		roundID:   roundID,
		propletID: propletID,
	}, nil
}

func decodeFLUpdateReq(_ context.Context, r *http.Request) (any, error) {
	if !strings.Contains(r.Header.Get("Content-Type"), api.ContentType) {
		return nil, errors.Join(apiutil.ErrValidation, apiutil.ErrUnsupportedContentType)
	}

	var update manager.FLUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		return nil, errors.Join(err, apiutil.ErrValidation)
	}

	return flUpdateReq{Update: update}, nil
}

func decodeFLUpdateCBORReq(_ context.Context, r *http.Request) (any, error) {
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/cbor" && contentType != "application/cbor-seq" {
		return nil, errors.Join(apiutil.ErrValidation, apiutil.ErrUnsupportedContentType)
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, errors.Join(err, apiutil.ErrValidation)
	}

	return data, nil
}

func decodeRoundStatusReq(_ context.Context, r *http.Request) (any, error) {
	roundID := chi.URLParam(r, "round_id")
	if roundID == "" {
		return nil, errors.Join(apiutil.ErrValidation, errors.New("round_id is required"))
	}

	return roundStatusReq{roundID: roundID}, nil
}

func decodeExperimentConfigReq(_ context.Context, r *http.Request) (any, error) {
	if !strings.Contains(r.Header.Get("Content-Type"), api.ContentType) {
		return nil, errors.Join(apiutil.ErrValidation, apiutil.ErrUnsupportedContentType)
	}

	var config manager.ExperimentConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		return nil, errors.Join(err, apiutil.ErrValidation)
	}

	return experimentConfigReq{Config: config}, nil
}
