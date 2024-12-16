package config

import (
	"errors"
	"fmt"
	"net/url"
)

type MQTTProxyConfig struct {
	BrokerURL string
	Password  string
	PropletID string
	ChannelID string
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
