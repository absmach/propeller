package worker

import (
	"fmt"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// PublishDiscovery publishes the discovery notification to the Manager.
func PublishDiscovery(client mqtt.Client, config *Config) {
	topic := fmt.Sprintf("channels/%s/messages/proplets", config.ChannelID)
	payload := fmt.Sprintf(`{"status":"online","PropletID":"%s","ChanID":"%s"}`, config.PropletID, config.ChannelID)
	token := client.Publish(topic, 0, false, payload)
	token.Wait()

	fmt.Println("Discovery message published.")
}
