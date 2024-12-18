package config

import (
	"errors"
	"fmt"
	"net/url"
)

type MQTTProxyConfig struct {
	BrokerURL string `env:"PROXY_MQTT_ADDRESS" envDefault:"tcp://localhost:1883"`
	Password  string `env:"PROXY_PROPLET_KEY"  envDefault:""`
	PropletID string `env:"PROXY_PROPLET_ID"   envDefault:""`
	ChannelID string `env:"PROXY_CHANNEL_ID"   envDefault:""`
}

func (c *MQTTProxyConfig) Validate() error {
	if c.BrokerURL == "" {
		return errors.New("broker_url is required")
	}
	if _, err := url.Parse(c.BrokerURL); err != nil {
		return fmt.Errorf("broker_url is not a valid URL: %w", err)
	}
	if c.Password == "" {
		return errors.New("password is required")
	}
	if c.PropletID == "" {
		return errors.New("proplet_id is required")
	}
	if c.ChannelID == "" {
		return errors.New("channel_id is required")
	}

	return nil
}
