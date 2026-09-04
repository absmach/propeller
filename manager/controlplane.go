// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package manager

import (
	"context"
	"encoding/json"

	"github.com/absmach/propeller/pkg/jsonrpc"
)

type Option func(*service)

func WithJSONRPCControlPlane(enabled bool) Option {
	return func(svc *service) {
		svc.jsonrpcControlPlane = enabled
	}
}

func (svc *service) publishControl(ctx context.Context, topic, method, taskID string, payload any) error {
	msg, err := svc.controlPayload(method, jsonrpc.StringID(taskID), payload)
	if err != nil {
		return err
	}

	return svc.pubsub.Publish(ctx, topic, msg)
}

func (svc *service) controlPayload(method string, id *jsonrpc.ID, payload any) (any, error) {
	if !svc.jsonrpcControlPlane {
		return payload, nil
	}

	req, err := jsonrpc.NewRequest(id, method, payload)
	if err != nil {
		return nil, err
	}

	return req, nil
}

func decodeControlMessage(msg map[string]any) (map[string]any, *jsonrpc.ID) {
	version, ok := msg["jsonrpc"].(string)
	if !ok || version != jsonrpc.Version {
		return msg, nil
	}

	var id *jsonrpc.ID
	if raw, ok := msg["id"]; ok {
		encoded, err := json.Marshal(raw)
		if err == nil {
			parsed := &jsonrpc.ID{}
			if err := parsed.UnmarshalJSON(encoded); err == nil {
				id = parsed
			}
		}
	}

	if result, ok := msg["result"].(map[string]any); ok {
		return result, id
	}

	if params, ok := msg["params"].(map[string]any); ok {
		return params, id
	}

	if rpcErr, ok := msg["error"].(map[string]any); ok {
		unwrapped := map[string]any{}
		if message, ok := rpcErr["message"].(string); ok {
			unwrapped["error"] = message
		}

		return unwrapped, id
	}

	return msg, id
}
