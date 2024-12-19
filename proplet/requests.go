package proplet

import "errors"

type startRequest struct {
	ID           string
	FunctionName string
	WasmFile     []byte
	Params       []uint64
}

func (r startRequest) Validate() error {
	if r.ID == "" {
		return errors.New("id is required")
	}
	if r.FunctionName == "" {
		return errors.New("function name is required")
	}
	if r.WasmFile == nil {
		return errors.New("wasm file is required")
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
