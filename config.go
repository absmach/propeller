package propeller

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml"
)

type Config struct {
	Manager ManagerConfig `toml:"manager"`
	Proplet PropletConfig `toml:"proplet"`
	Proxy   ProxyConfig   `toml:"proxy"`
}

type ManagerConfig struct {
	DomainID    string `toml:"domain_id"`
	ClientID    string `toml:"client_id"`
	ClientKey   string `toml:"client_key"`
	ChannelID   string `toml:"channel_id"`
	WorkloadKey string `toml:"workload_key"` // Key used to encrypt workloads before sending
}

type PropletConfig struct {
	DomainID    string `toml:"domain_id"`
	ClientID    string `toml:"client_id"`
	ClientKey   string `toml:"client_key"`
	ChannelID   string `toml:"channel_id"`
	WorkloadKey string `toml:"workload_key"` // Key used to decrypt workloads upon receipt
}

type ProxyConfig struct {
	DomainID    string `toml:"domain_id"`
	ClientID    string `toml:"client_id"`
	ClientKey   string `toml:"client_key"`
	ChannelID   string `toml:"channel_id"`
	WorkloadKey string `toml:"workload_key"` // Key used if proxy needs to handle encrypted streams
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	tree, err := toml.Load(string(data))
	if err != nil {
		return nil, fmt.Errorf("error parsing config file: %w", err)
	}

	var cfg Config
	if err := tree.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return &cfg, nil
}
