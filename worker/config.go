package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Config represents the configuration structure for the Proplet.
type Config struct {
	BrokerURL string `json:"brokerURL"`
	Token     string `json:"token"`
	PropletID string `json:"propletID"`
	ChannelID string `json:"channelID"`
}

func LoadConfig(filepath string) (*Config, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("unable to open configuration file: %w", err)
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode configuration: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &config, nil
}

func (c *Config) Validate() error {
	if c.BrokerURL == "" {
		return errors.New("brokerURL must not be empty")
	}
	if c.PropletID == "" {
		return errors.New("propletID must not be empty")
	}
	if c.ChannelID == "" {
		return errors.New("channelID must not be empty")
	}
	return nil
}
