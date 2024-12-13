package proplet

import (
	"errors"
	"net/url"
	"time"
)

type Config struct {
	LogLevel           string        `json:"log_level"`
	ID                 string        `json:"id"`
	MQTTAddress        string        `json:"mqtt_address"`
	MQTTTimeout        time.Duration `json:"mqtt_timeout"`
	MQTTQoS            byte          `json:"mqtt_qos"`
	LivelinessInterval time.Duration `json:"liveliness_interval"`
	RegistryURL        string        `json:"registry_url,omitempty"`
	RegistryToken      string        `json:"registry_token,omitempty"`
	RegistryTimeout    time.Duration `json:"registry_timeout,omitempty"`
	ChannelID          string        `json:"channel_id"`
	ThingID            string        `json:"thing_id"`
	ThingKey           string        `json:"thing_key"`
}

func (c Config) Validate() error {
	if c.MQTTAddress == "" {
		return errors.New("MQTT address is required")
	}
	if _, err := url.Parse(c.MQTTAddress); err != nil {
		return errors.Join(errors.New("MQTT address is not a valid URL"), err)
	}
	if c.ChannelID == "" {
		return errors.New("magistrala channel id is required")
	}
	if c.ThingID == "" {
		return errors.New("magistrala thing id is required")
	}
	if c.ThingKey == "" {
		return errors.New("magistrala thing key is required")
	}
	if _, err := url.Parse(c.RegistryURL); err != nil && c.RegistryURL != "" {
		return errors.Join(errors.New("registry URL is not a valid URL"), err)
	}

	return nil
}
