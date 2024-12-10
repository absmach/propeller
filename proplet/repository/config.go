package repository

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
)

// Config holds configuration for the MQTT client.
type Config struct {
	BrokerURL     string `json:"broker_url"`
	Password      string `json:"password"`
	PropletID     string `json:"proplet_id"`
	ChannelID     string `json:"channel_id"`
	RegistryURL   string `json:"registry_url"`
	RegistryToken string `json:"registry_token"`
}

// LoadConfig loads and validates the configuration from a JSON file.
func LoadConfig(filepath string, hasWASMFile bool) (*Config, error) {
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

	if err := config.Validate(hasWASMFile); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &config, nil
}

// Validate ensures that all required fields are set and correct.
func (c *Config) Validate(hasWASMFile bool) error {
	if c.BrokerURL == "" {
		return fmt.Errorf("missing required field: broker_url")
	}
	if _, err := url.ParseRequestURI(c.BrokerURL); err != nil {
		return fmt.Errorf("invalid broker_url '%s': %w", c.BrokerURL, err)
	}

	if c.Password == "" {
		return fmt.Errorf("missing required field: password")
	}

	if c.PropletID == "" {
		return fmt.Errorf("missing required field: proplet_id")
	}

	if c.ChannelID == "" {
		return fmt.Errorf("missing required field: channel_id")
	}

	// RegistryURL is only required if no WASM file is provided
	if c.RegistryURL == "" && !hasWASMFile {
		return fmt.Errorf("missing required field: registry_url")
	}
	if c.RegistryURL != "" {
		if _, err := url.ParseRequestURI(c.RegistryURL); err != nil {
			return fmt.Errorf("invalid registry_url '%s': %w", c.RegistryURL, err)
		}
	}

	if c.RegistryToken == "" && c.RegistryURL != "" {
		return fmt.Errorf("missing required field: registry_token")
	}

	return nil
}
