package worker

import (
	"fmt"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// ValidateConfig validates the essential fields in the Config struct.
func ValidateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("config is nil: %w", pkgerrors.ErrConfigValidation)
	}
	if config.ChannelID == "" {
		return fmt.Errorf("ChannelID is missing: %w", pkgerrors.ErrMissingChannelID)
	}
	if config.PropletID == "" {
		return fmt.Errorf("PropletID is missing: %w", pkgerrors.ErrMissingPropletID)
	}
	return nil
}

// PublishDiscovery publishes the discovery notification to the Manager.
func PublishDiscovery(client mqtt.Client, config *Config) error {
	// Validate the MQTT client and configuration
	if client == nil {
		return fmt.Errorf("MQTT client is nil: %w", pkgerrors.ErrNilMQTTClient)
	}
	if err := ValidateConfig(config); err != nil {
		return fmt.Errorf("failed to validate configuration: %w", err)
	}

	// Construct the topic and payload
	topic := fmt.Sprintf("channels/%s/messages/proplets", config.ChannelID)
	payload := fmt.Sprintf(`{"status":"online","PropletID":"%s","ChanID":"%s"}`, config.PropletID, config.ChannelID)

	// Publish the discovery message
	token := client.Publish(topic, 0, false, payload)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("failed to publish discovery message to topic '%s': %w", topic, pkgerrors.ErrPublishDiscovery)
	}

	fmt.Printf("Discovery message published to topic '%s'\n", topic)
	return nil
}
