package api

import (
	"fmt"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
)

type Response struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (r Response) Validate() error {
	if r.Status == "" {
		return fmt.Errorf("response: status is required but missing: %w", pkgerrors.ErrMissingValue)
	}
	if r.Status != "success" && r.Status != "failure" {
		return fmt.Errorf("response: invalid status '%s': %w", r.Status, pkgerrors.ErrInvalidStatus)
	}
	if r.Status == "failure" && r.Error == "" {
		return fmt.Errorf("response: error message is required for failure status: %w", pkgerrors.ErrInvalidValue)
	}

	return nil
}

type RPCResponse struct {
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	ID     int    `json:"id"`
}

func (r RPCResponse) Validate() error {
	if r.ID == 0 {
		return fmt.Errorf("RPC response: ID is required but missing or zero: %w", pkgerrors.ErrMissingValue)
	}
	if r.Error != "" && r.Result != "" {
		return fmt.Errorf("RPC response: both result and error cannot be set simultaneously: %w", pkgerrors.ErrInvalidValue)
	}
	if r.Error == "" && r.Result == "" {
		return fmt.Errorf("RPC response: result or error must be set: %w", pkgerrors.ErrMissingResult)
	}

	return nil
}

func NewSuccessResponse() Response {
	return Response{
		Status: "success",
	}
}

func NewFailureResponse(err error) Response {
	return Response{
		Status: "failure",
		Error:  err.Error(),
	}
}

func NewRPCSuccessResponse(id int, result string) RPCResponse {
	return RPCResponse{
		ID:     id,
		Result: result,
	}
}

func NewRPCFailureResponse(id int, err error) RPCResponse {
	return RPCResponse{
		ID:    id,
		Error: err.Error(),
	}
}
