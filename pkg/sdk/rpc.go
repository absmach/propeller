package sdk

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/absmach/propeller/pkg/jsonrpc"
)

var rpcCounter atomic.Int64

func (sdk *propSDK) Call(method string, params any) (json.RawMessage, error) {
	req, err := jsonrpc.NewRequest(jsonrpc.NumberID(rpcCounter.Add(1)), method, params)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	body, err := sdk.processRequest(http.MethodPost, sdk.managerURL+"/rpc", data, http.StatusOK)
	if err != nil {
		return nil, err
	}

	var resp jsonrpc.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}

	return resp.Result, nil
}

func (sdk *propSDK) CallBatch(calls []RPCCall) ([]jsonrpc.Response, error) {
	if len(calls) == 0 {
		return nil, errors.New("batch must not be empty")
	}

	requests := make([]*jsonrpc.Request, 0, len(calls))
	for _, call := range calls {
		req, err := jsonrpc.NewRequest(jsonrpc.NumberID(rpcCounter.Add(1)), call.Method, call.Params)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}

	data, err := json.Marshal(requests)
	if err != nil {
		return nil, err
	}

	body, err := sdk.processRequest(http.MethodPost, sdk.managerURL+"/rpc", data, http.StatusOK)
	if err != nil {
		return nil, err
	}

	var responses []jsonrpc.Response
	if err := json.Unmarshal(body, &responses); err != nil {
		return nil, err
	}

	return responses, nil
}

type RPCCall struct {
	Method string
	Params any
}
