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
	if err := validateField(c.BrokerURL, "broker_url"); err != nil {
		return err
	}
	if err := validateURL(c.BrokerURL); err != nil {
		return err
	}

	if err := validateField(c.Password, "password"); err != nil {
		return err
	}

	if err := validateField(c.PropletID, "proplet_id"); err != nil {
		return err
	}

	if err := validateField(c.ChannelID, "channel_id"); err != nil {
		return err
	}

	if c.RegistryURL != "" {
		if err := validateURL(c.RegistryURL); err != nil {
			return err
		}
	}
	if c.RegistryToken != "" {
		if err := validateField(c.RegistryToken, "registry_token"); err != nil {
			return err
		}
	}

	return nil
}

func validateField(field, fieldName string) error {
	if field == "" {
		return errors.New("missing required field: " + fieldName)
	}

	return nil
}

func validateURL(field string) error {
	if _, err := url.ParseRequestURI(field); err != nil {
		return fmt.Errorf("invalid URL '%s': %w", field, err)
	}

	return nil
}
