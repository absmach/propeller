// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
)

type Handler func(ctx context.Context, params json.RawMessage) (any, error)

type Server struct {
	mu      sync.RWMutex
	methods map[string]Handler
}

func NewServer() *Server {
	return &Server{methods: make(map[string]Handler)}
}

func (s *Server) Register(method string, handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.methods[method] = handler
}

func (s *Server) Methods() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	methods := make([]string, 0, len(s.methods))
	for method := range s.methods {
		methods = append(methods, method)
	}
	sort.Strings(methods)

	return methods
}

func (s *Server) Handle(ctx context.Context, data []byte) []byte {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return marshalResponse(NewErrorResponse(nil, ParseError(nil)))
	}

	if trimmed[0] != '[' {
		return marshalResponse(s.handleOne(ctx, trimmed))
	}

	var batch []json.RawMessage
	if err := json.Unmarshal(trimmed, &batch); err != nil {
		return marshalResponse(NewErrorResponse(nil, InvalidRequest(err.Error())))
	}
	if len(batch) == 0 {
		return marshalResponse(NewErrorResponse(nil, InvalidRequest("batch must not be empty")))
	}

	responses := make([]*Response, 0, len(batch))
	for _, raw := range batch {
		if resp := s.handleOne(ctx, raw); resp != nil {
			responses = append(responses, resp)
		}
	}
	if len(responses) == 0 {
		return nil
	}

	out, err := json.Marshal(responses)
	if err != nil {
		return marshalResponse(NewErrorResponse(nil, InternalError(err.Error())))
	}

	return out
}

func (s *Server) handleOne(ctx context.Context, raw []byte) *Response {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return NewErrorResponse(nil, InvalidRequest(decodeMessage(raw, err)))
	}
	if err := req.Validate(); err != nil {
		return NewErrorResponse(req.ID, InvalidRequest(err.Error()))
	}

	s.mu.RLock()
	handler, ok := s.methods[req.Method]
	s.mu.RUnlock()

	if !ok {
		if req.IsNotification() {
			return nil
		}

		return NewErrorResponse(req.ID, MethodNotFound(req.Method))
	}

	result, err := handler(ctx, req.Params)
	if req.IsNotification() {
		return nil
	}
	if err != nil {
		return NewErrorResponse(req.ID, ErrorFrom(err))
	}

	resp, err := NewResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, InternalError(err.Error()))
	}

	return resp
}

func decodeMessage(raw []byte, err error) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "request must be a JSON object"
	}
	if errors.Is(err, ErrInvalidID) {
		return ErrInvalidID.Error()
	}

	return "request could not be decoded"
}

func marshalResponse(resp *Response) []byte {
	if resp == nil {
		return nil
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":null}`)
	}

	return out
}
