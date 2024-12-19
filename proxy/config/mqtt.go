package config

type MQTTProxyConfig struct {
	BrokerURL string `env:"PROXY_MQTT_ADDRESS"          envDefault:"tcp://localhost:1883"`
	Password  string `env:"PROXY_PROPLET_KEY,notEmpty"`
	PropletID string `env:"PROXY_PROPLET_ID,notEmpty" `
	ChannelID string `env:"PROXY_CHANNEL_ID,notEmpty"`
}
