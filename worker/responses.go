package worker

type Response struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type RPCResponse struct {
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	ID     int    `json:"id"`
}
