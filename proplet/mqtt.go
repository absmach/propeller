package proplet

var (
	RegistryAckTopicTemplate  = "channels/%s/messages/control/manager/registry"
	aliveTopicTemplate        = "channels/%s/messages/control/proplet/alive"
	discoveryTopicTemplate    = "channels/%s/messages/control/proplet/create"
	startTopicTemplate        = "channels/%s/messages/control/manager/start"
	stopTopicTemplate         = "channels/%s/messages/control/manager/stop"
	registryResponseTopic     = "channels/%s/messages/registry/server"
	fetchRequestTopicTemplate = "channels/%s/messages/registry/proplet"
)
