package worker

type DeployAppResponse struct {
	Err string `json:"error,omitempty"`
}

type StopAppResponse struct {
	Err string `json:"error,omitempty"`
}

type PublishDiscoveryResponse struct {
	Err string `json:"error,omitempty"`
}

type ListenForAppChunksResponse struct {
	Err string `json:"error,omitempty"`
}

type SendTelemetryResponse struct {
	Err string `json:"error,omitempty"`
}

type HandleRPCCommandResponse struct {
	Err string `json:"error,omitempty"`
}
