package config

import (
	"github.com/caarlos0/env/v11"
)

type MQTTProxyConfig struct {
	BrokerURL string `env:"BROKER_URL"  envDefault:""`
	Password  string `env:"PASSWORD"    envDefault:""`
	PropletID string `env:"PROPLET_ID"  envDefault:""`
	ChannelID string `env:"CHANNEL_ID"  envDefault:""`
}

func LoadMQTTConfig(opts env.Options) (*MQTTProxyConfig, error) {
	c := MQTTProxyConfig{}
	if err := env.ParseWithOptions(&c, opts); err != nil {
		return nil, err
	}

	return &c, nil
}
