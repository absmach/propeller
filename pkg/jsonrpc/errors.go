// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"errors"
	"fmt"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
)

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	CodeNotFound    = -32001
	CodeConflict    = -32002
	CodeValidation  = -32003
	CodeTimeout     = -32004
	CodeUnavailable = -32005
)

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func NewError(code int, message string, data any) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

func (e *Error) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

func ParseError(data any) *Error {
	return NewError(CodeParseError, "Parse error", data)
}

func InvalidRequest(data any) *Error {
	return NewError(CodeInvalidRequest, "Invalid Request", data)
}

func MethodNotFound(method string) *Error {
	return NewError(CodeMethodNotFound, "Method not found", method)
}

func InvalidParams(data any) *Error {
	return NewError(CodeInvalidParams, "Invalid params", data)
}

func InternalError(data any) *Error {
	return NewError(CodeInternalError, "Internal error", data)
}

func ErrorFrom(err error) *Error {
	if err == nil {
		return nil
	}

	var rpcErr *Error
	if errors.As(err, &rpcErr) {
		return rpcErr
	}

	switch {
	case errors.Is(err, pkgerrors.ErrNotFound):
		return NewError(CodeNotFound, err.Error(), nil)
	case errors.Is(err, pkgerrors.ErrConflict), errors.Is(err, pkgerrors.ErrEntityExists):
		return NewError(CodeConflict, err.Error(), nil)
	case errors.Is(err, pkgerrors.ErrInvalidParams), errors.Is(err, pkgerrors.ErrInvalidData):
		return NewError(CodeInvalidParams, err.Error(), nil)
	case errors.Is(err, pkgerrors.ErrInvalidValue),
		errors.Is(err, pkgerrors.ErrMissingValue),
		errors.Is(err, pkgerrors.ErrEmptyKey):
		return NewError(CodeValidation, err.Error(), nil)
	case errors.Is(err, pkgerrors.ErrInvalidMethod):
		return NewError(CodeMethodNotFound, err.Error(), nil)
	case errors.Is(err, pkgerrors.ErrTimeout):
		return NewError(CodeTimeout, err.Error(), nil)
	default:
		return NewError(CodeInternalError, err.Error(), nil)
	}
}
