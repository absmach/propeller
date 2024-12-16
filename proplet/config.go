package proplet

import (
	"errors"
	"net/url"
	"time"
)

type Config struct {
	LogLevel           string
	InstanceID         string
	MQTTAddress        string
	MQTTTimeout        time.Duration
	MQTTQoS            byte
	LivelinessInterval time.Duration
	RegistryURL        string
	RegistryToken      string
	RegistryTimeout    time.Duration
	ChannelID          string
	ThingID            string
	ThingKey           string
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
