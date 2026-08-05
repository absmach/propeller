// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"bytes"
	"encoding/json"
	"errors"
)

const Version = "2.0"

var (
	ErrInvalidVersion = errors.New("jsonrpc version must be 2.0")
	ErrMissingMethod  = errors.New("method is required")
	ErrInvalidID      = errors.New("id must be a string, a number, or null")
	ErrInvalidParams  = errors.New("params must be an object or an array")
)

type ID struct {
	raw json.RawMessage
}

func StringID(s string) *ID {
	raw, err := json.Marshal(s)
	if err != nil {
		return &ID{}
	}

	return &ID{raw: raw}
}

func NumberID(n int64) *ID {
	raw, err := json.Marshal(n)
	if err != nil {
		return &ID{}
	}

	return &ID{raw: raw}
}

func NullID() *ID {
	return &ID{raw: json.RawMessage("null")}
}

func (id *ID) MarshalJSON() ([]byte, error) {
	if id == nil || len(id.raw) == 0 {
		return []byte("null"), nil
	}

	return id.raw, nil
}

func (id *ID) UnmarshalJSON(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}

	switch v.(type) {
	case string, float64, nil:
		id.raw = append(json.RawMessage(nil), bytes.TrimSpace(data)...)

		return nil
	default:
		return ErrInvalidID
	}
}

func (id *ID) String() string {
	if id.IsNull() {
		return "null"
	}

	var s string
	if err := json.Unmarshal(id.raw, &s); err == nil {
		return s
	}

	return string(id.raw)
}

func (id *ID) IsNull() bool {
	return id == nil || len(id.raw) == 0 || bytes.Equal(bytes.TrimSpace(id.raw), []byte("null"))
}

func decodeID(data []byte, target **ID) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}

	raw, ok := probe["id"]
	if !ok {
		*target = nil

		return nil
	}

	id := &ID{}
	if err := id.UnmarshalJSON(raw); err != nil {
		return err
	}
	*target = id

	return nil
}

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      *ID             `json:"id,omitempty"`
}

func NewRequest(id *ID, method string, params any) (*Request, error) {
	req := &Request{
		JSONRPC: Version,
		Method:  method,
		ID:      id,
	}
	if params == nil {
		return req, nil
	}

	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	req.Params = raw

	return req, nil
}

func (r *Request) UnmarshalJSON(data []byte) error {
	type alias Request
	aux := (*alias)(r)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	return decodeID(data, &r.ID)
}

func (r *Request) IsNotification() bool {
	return r.ID == nil
}

func (r *Request) Validate() error {
	if r.JSONRPC != Version {
		return ErrInvalidVersion
	}
	if r.Method == "" {
		return ErrMissingMethod
	}
	if len(r.Params) == 0 {
		return nil
	}

	trimmed := bytes.TrimSpace(r.Params)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return ErrInvalidParams
	}

	return nil
}

func (r *Request) UnmarshalParams(v any) error {
	trimmed := bytes.TrimSpace(r.Params)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}

	return json.Unmarshal(trimmed, v)
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	ID      *ID             `json:"id"`
}

func NewResponse(id *ID, result any) (*Response, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	return &Response{
		JSONRPC: Version,
		Result:  raw,
		ID:      id,
	}, nil
}

func NewErrorResponse(id *ID, rpcErr *Error) *Response {
	if id == nil {
		id = NullID()
	}

	return &Response{
		JSONRPC: Version,
		Error:   rpcErr,
		ID:      id,
	}
}

func (r *Response) UnmarshalJSON(data []byte) error {
	type alias Response
	aux := (*alias)(r)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	return decodeID(data, &r.ID)
}

func (r *Response) UnmarshalResult(v any) error {
	if r.Error != nil {
		return r.Error
	}
	trimmed := bytes.TrimSpace(r.Result)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}

	return json.Unmarshal(trimmed, v)
}
