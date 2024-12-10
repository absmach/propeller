package api

import (
	"fmt"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
)

// StartRequest represents a request to start an application.
type StartRequest struct {
	AppName string   `json:"app_name"`
	Params  []string `json:"params"`
}

func (r *StartRequest) Validate() error {
	if r.AppName == "" {
		return fmt.Errorf("start request: app_name is required but missing: %w", pkgerrors.ErrMissingAppName)
	}
	return nil
}

// StopRequest represents a request to stop an application.
type StopRequest struct {
	AppName string `json:"app_name"`
}

func (r *StopRequest) Validate() error {
	if r.AppName == "" {
		return fmt.Errorf("stop request: app_name is required but missing: %w", pkgerrors.ErrMissingAppName)
	}
	return nil
}

// RPCRequest represents a generic RPC request.
type RPCRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
	ID     int           `json:"id"`
}

func (r *RPCRequest) Validate() error {
	if r.Method == "" {
		return fmt.Errorf("RPC request: method is required but missing: %w", pkgerrors.ErrInvalidMethod)
	}
	if len(r.Params) == 0 {
		return fmt.Errorf("RPC request: params are required but missing: %w", pkgerrors.ErrInvalidParams)
	}
	return nil
}

// ParseParams parses and validates parameters for specific methods.
func (r *RPCRequest) ParseParams() (interface{}, error) {
	switch r.Method {
	case "start":
		if len(r.Params) < 1 {
			return nil, fmt.Errorf("start method: missing required parameters: %w", pkgerrors.ErrInvalidParams)
		}
		appName, ok := r.Params[0].(string)
		if !ok || appName == "" {
			return nil, fmt.Errorf("start method: invalid app_name parameter: %w", pkgerrors.ErrInvalidParams)
		}
		return StartRequest{
			AppName: appName,
			Params:  parseStringSlice(r.Params[1:]),
		}, nil
	case "stop":
		if len(r.Params) < 1 {
			return nil, fmt.Errorf("stop method: missing required parameters: %w", pkgerrors.ErrInvalidParams)
		}
		appName, ok := r.Params[0].(string)
		if !ok || appName == "" {
			return nil, fmt.Errorf("stop method: invalid app_name parameter: %w", pkgerrors.ErrInvalidParams)
		}
		return StopRequest{AppName: appName}, nil
	default:
		return nil, fmt.Errorf("unknown method '%s': %w", r.Method, pkgerrors.ErrInvalidMethod)
	}
}

// Helper function to parse a slice of parameters into a string slice.
func parseStringSlice(params []interface{}) []string {
	result := []string{}
	for _, param := range params {
		if str, ok := param.(string); ok {
			result = append(result, str)
		}
	}
	return result
}
