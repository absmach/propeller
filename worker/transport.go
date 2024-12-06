package worker

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	kithttp "github.com/go-kit/kit/transport/http"
)

func MakeHTTPHandler(s Service) http.Handler {
	r := chi.NewRouter()

	r.Method("POST", "/deploy", kithttp.NewServer(
		DeployAppEndpoint(s),
		decodeDeployAppRequest,
		encodeResponse,
	))

	r.Method("POST", "/stop", kithttp.NewServer(
		StopAppEndpoint(s),
		decodeStopAppRequest,
		encodeResponse,
	))

	r.Method("POST", "/publish_discovery", kithttp.NewServer(
		PublishDiscoveryEndpoint(s),
		decodePublishDiscoveryRequest,
		encodeResponse,
	))

	r.Method("POST", "/listen_for_chunks", kithttp.NewServer(
		ListenForAppChunksEndpoint(s),
		decodeListenForAppChunksRequest,
		encodeResponse,
	))

	r.Method("POST", "/send_telemetry", kithttp.NewServer(
		SendTelemetryEndpoint(s),
		decodeSendTelemetryRequest,
		encodeResponse,
	))

	r.Method("POST", "/handle_rpc", kithttp.NewServer(
		HandleRPCCommandEndpoint(s),
		decodeHandleRPCCommandRequest,
		encodeResponse,
	))

	return r
}

func decodeDeployAppRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req DeployAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeStopAppRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req StopAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodePublishDiscoveryRequest(_ context.Context, r *http.Request) (interface{}, error) {
	return PublishDiscoveryRequest{}, nil
}

func decodeListenForAppChunksRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req ListenForAppChunksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeSendTelemetryRequest(_ context.Context, r *http.Request) (interface{}, error) {
	return SendTelemetryRequest{}, nil
}

func decodeHandleRPCCommandRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var req HandleRPCCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func encodeResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(response)
}
