package worker

type DeployAppRequest struct {
	AppName string `json:"appName"`
}

type StopAppRequest struct {
	AppName string `json:"appName"`
}

type PublishDiscoveryRequest struct{}

type ListenForAppChunksRequest struct {
	AppName string `json:"appName"`
}

type SendTelemetryRequest struct{}

type HandleRPCCommandRequest struct {
	Command string   `json:"command"`
	Params  []string `json:"params"`
}
