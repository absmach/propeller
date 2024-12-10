package config

type MQTTProxyConfig struct {
	BrokerURL string `json:"broker_url"`
	Password  string `json:"password"`
	PropletID string `json:"proplet_id"`
	ChannelID string `json:"channel_id"`
}
