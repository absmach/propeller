package config

import (
	"github.com/caarlos0/env/v11"
)

type MQTTProxyConfig struct {
	BrokerURL string `json:"broker_url"  env:"BROKER_URL"`
	Password  string `json:"password"    env:"PASSWORD"`
	PropletID string `json:"proplet_id"  env:"PROPLET_ID"`
	ChannelID string `json:"channel_id"  env:"CHANNEL_ID"`
}

func LoadMQTTConfig(opts env.Options) (*MQTTProxyConfig, error) {
	c := MQTTProxyConfig{}
	if err := env.ParseWithOptions(&c, opts); err != nil {
		return nil, err
	}

	return &c, nil
}
