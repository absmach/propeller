package worker

import (
	"encoding/json"
	"os"
)

type Config struct {
	BrokerURL string `json:"brokerURL"`
	Token     string `json:"token"`
	PropletID string `json:"propletID"`
	ChannelID string `json:"channelID"`
}

func LoadConfig(filepath string) (*Config, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}
