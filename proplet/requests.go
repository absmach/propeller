package proplet

import (
	"errors"

	"github.com/absmach/propeller/task"
)

type startRequest struct {
	ID           string
	FunctionName string
	WasmFile     []byte
	imageURL     task.URLValue
	Params       []uint64
}

func (r startRequest) Validate() error {
	if r.ID == "" {
		return errors.New("id is required")
	}
	if r.FunctionName == "" {
		return errors.New("function name is required")
	}
	if r.WasmFile == nil && r.imageURL == (task.URLValue{}) {
		return errors.New("either wasm file or wasm file download path is required")
	}

	return nil
}

type stopRequest struct {
	ID string
}

func (r stopRequest) Validate() error {
	if r.ID == "" {
		return errors.New("id is required")
	}

	return nil
}
