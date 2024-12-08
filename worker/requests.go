package worker

type StartRequest struct {
	AppName string   `json:"app_name"`
	Params  []string `json:"params"`
}

type StopRequest struct {
	AppName string `json:"app_name"`
}

type RPCRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
	ID     int           `json:"id"`
}
