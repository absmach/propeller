package proplet

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
)

// Config holds configuration for the MQTT client.
type Config struct {
	BrokerURL string `json:"brokerURL"`
	Token     string `json:"token"`
	PropletID string `json:"propletID"`
	ChannelID string `json:"channelID"`
}

// LoadConfig loads and validates the configuration from a JSON file.
func LoadConfig(filepath string) (*Config, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("unable to open configuration file '%s': %w", filepath, err)
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to parse configuration file '%s': %w", filepath, err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &config, nil
}

// Validate ensures that all required fields are set and correct.
func (c *Config) Validate() error {
	if c.BrokerURL == "" || c.PropletID == "" || c.ChannelID == "" {
		return fmt.Errorf("missing required configuration fields")
	}
	if _, err := url.ParseRequestURI(c.BrokerURL); err != nil {
		return fmt.Errorf("invalid broker URL '%s': %w", c.BrokerURL, err)
	}
	return nil
}
