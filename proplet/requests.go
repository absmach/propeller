package proplet

import (
	"errors"
)

type startRequest struct {
	ID           string
	CLIArgs      []string
	FunctionName string
	WasmFile     []byte
	imageURL     string
	Params       []uint64
	Daemon       bool
	Env          map[string]string
}

func (r startRequest) Validate() error {
	if r.ID == "" {
		return errors.New("id is required")
	}
	if r.FunctionName == "" {
		return errors.New("function name is required")
	}
	if r.WasmFile == nil && r.imageURL == "" {
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
