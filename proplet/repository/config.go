package repository

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

func (c *Config) Validate(hasWASMFile bool) error {
	if err := c.validateField(c.BrokerURL, "broker_url"); err != nil {
		return err
	}
	if err := c.validateURL(c.BrokerURL); err != nil {
		return err
	}

	if err := c.validateField(c.Password, "password"); err != nil {
		return err
	}

	if err := c.validateField(c.PropletID, "proplet_id"); err != nil {
		return err
	}

	if err := c.validateField(c.ChannelID, "channel_id"); err != nil {
		return err
	}

	if err := c.validateRegistryURL(c.RegistryURL, hasWASMFile); err != nil {
		return err
	}

	if err := c.validateURL(c.RegistryURL); err != nil {
		return err
	}

	if err := c.validateField(c.RegistryToken, "registry_token"); err != nil {
		return err
	}

	return nil
}

func (c *Config) validateField(field, fieldName string) error {
	if field == "" {
		return errors.New("missing required field: " + fieldName)
	}

	return nil
}

func (c *Config) validateURL(field string) error {
	if _, err := url.ParseRequestURI(field); err != nil {
		return fmt.Errorf("invalid URL '%s': %w", field, err)
	}

	return nil
}

func (c *Config) validateRegistryURL(registryURL string, hasWASMFile bool) error {
	if registryURL == "" && !hasWASMFile {
		return errors.New("missing required field: registry_url")
	}

	return nil
}
