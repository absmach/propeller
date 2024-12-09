package proplet

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
)

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
		return nil, fmt.Errorf("unable to open configuration file '%s': %w", filepath, pkgerrors.ErrInvalidData)
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to parse configuration file '%s': %w", filepath, pkgerrors.ErrInvalidData)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &config, nil
}

// Validate ensures that all required fields are set and correct.
func (c *Config) Validate() error {
	if c.BrokerURL == "" {
		return fmt.Errorf("brokerURL is required but missing: %w", pkgerrors.ErrMissingValue)
	}
	if _, err := url.ParseRequestURI(c.BrokerURL); err != nil {
		return fmt.Errorf("brokerURL '%s' is not a valid URL: %w", c.BrokerURL, pkgerrors.ErrInvalidValue)
	}
	if c.PropletID == "" {
		return fmt.Errorf("propletID is required but missing: %w", pkgerrors.ErrMissingValue)
	}
	if c.ChannelID == "" {
		return fmt.Errorf("channelID is required but missing: %w", pkgerrors.ErrMissingValue)
	}
	return nil
}
