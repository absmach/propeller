package config

type MQTTProxyConfig struct {
	BrokerURL string `env:"PROXY_MQTT_ADDRESS"         envDefault:"tcp://localhost:1883"`
	Password  string `env:"PROXY_PROPLET_KEY,notEmpty" envDefault:""`
	PropletID string `env:"PROXY_PROPLET_ID,notEmpty"  envDefault:""`
	ChannelID string `env:"PROXY_CHANNEL_ID,notEmpty"  envDefault:""`
}
