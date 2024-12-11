package proplet

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
)

type Config struct {
	BrokerURL     string `json:"broker_url"`
	Password      string `json:"password"`
	PropletID     string `json:"proplet_id"`
	ChannelID     string `json:"channel_id"`
	RegistryURL   string `json:"registry_url"`
	RegistryToken string `json:"registry_token"`
}

func LoadConfig(filepath string, hasWASMFile bool) (Config, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return Config{}, fmt.Errorf("unable to open configuration file '%s': %w", filepath, err)
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("failed to parse configuration file '%s': %w", filepath, err)
	}

	if err := config.Validate(hasWASMFile); err != nil {
		return Config{}, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

func (c Config) Validate(hasWASMFile bool) error {
	requiredFields := map[string]string{
		"broker_url": c.BrokerURL,
		"password":   c.Password,
		"proplet_id": c.PropletID,
		"channel_id": c.ChannelID,
	}

	for fieldName, value := range requiredFields {
		if value == "" {
			return fmt.Errorf("%s is required", fieldName)
		}
	}

	if _, err := url.Parse(c.BrokerURL); err != nil {
		return fmt.Errorf("broker_url is not a valid URL: %w", err)
	}

	if !hasWASMFile {
		if c.RegistryURL == "" || c.RegistryToken == "" {
			return errors.New("registry_url and registry_token are required when not using a WASM file")
		}
	}

	if c.RegistryURL != "" {
		if _, err := url.Parse(c.RegistryURL); err != nil {
			return fmt.Errorf("registry_url is not a valid URL: %w", err)
		}
		if c.RegistryToken == "" {
			return errors.New("registry_token is required when a registry_url is provided")
		}
	}

	return nil
}
