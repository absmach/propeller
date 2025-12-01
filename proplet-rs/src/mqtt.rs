use anyhow::{Context, Result};
use rumqttc::{AsyncClient, Event, EventLoop, MqttOptions, Packet, QoS};
use serde::{de::DeserializeOwned, Serialize};
use std::time::Duration;
use tokio::sync::mpsc;
use tracing::{debug, error, info, warn};

pub struct MqttConfig {
    pub address: String,
    pub client_id: String,
    pub timeout: Duration,
    pub qos: QoS,
}

#[derive(Clone)]
pub struct PubSub {
    client: AsyncClient,
}

impl PubSub {
    pub async fn new(config: MqttConfig) -> Result<(Self, EventLoop)> {
        let mut mqtt_options = MqttOptions::new(
            config.client_id,
            config.address.split("://").nth(1).unwrap_or(&config.address),
            1883,
        );

        mqtt_options.set_keep_alive(Duration::from_secs(30));

        let (client, eventloop) = AsyncClient::new(mqtt_options, 10);

        info!("MQTT client created");

        Ok((Self { client }, eventloop))
    }

    pub async fn publish<T: Serialize>(&self, topic: &str, payload: &T) -> Result<()> {
        let json = serde_json::to_vec(payload)
            .context("Failed to serialize payload")?;

        self.client
            .publish(topic, QoS::ExactlyOnce, false, json)
            .await
            .context("Failed to publish message")?;

        debug!("Published to topic: {}", topic);
        Ok(())
    }

    pub async fn subscribe(&self, topic: &str) -> Result<()> {
        self.client
            .subscribe(topic, QoS::ExactlyOnce)
            .await
            .context(format!("Failed to subscribe to topic: {}", topic))?;

        info!("Subscribed to topic: {}", topic);
        Ok(())
    }

    pub async fn unsubscribe(&self, topic: &str) -> Result<()> {
        self.client
            .unsubscribe(topic)
            .await
            .context(format!("Failed to unsubscribe from topic: {}", topic))?;

        info!("Unsubscribed from topic: {}", topic);
        Ok(())
    }

    pub fn disconnect(&self) -> Result<()> {
        // rumqttc handles disconnection automatically when dropped
        info!("Disconnecting MQTT client");
        Ok(())
    }
}

pub struct MqttMessage {
    pub topic: String,
    pub payload: Vec<u8>,
}

impl MqttMessage {
    pub fn decode<T: DeserializeOwned>(&self) -> Result<T> {
        serde_json::from_slice(&self.payload)
            .context("Failed to deserialize message payload")
    }
}

pub async fn process_mqtt_events(
    mut eventloop: EventLoop,
    tx: mpsc::UnboundedSender<MqttMessage>,
) {
    info!("Starting MQTT event loop");

    loop {
        match eventloop.poll().await {
            Ok(Event::Incoming(Packet::Publish(publish))) => {
                let msg = MqttMessage {
                    topic: publish.topic.clone(),
                    payload: publish.payload.to_vec(),
                };

                if let Err(e) = tx.send(msg) {
                    error!("Failed to send MQTT message to handler: {}", e);
                }
            }
            Ok(Event::Incoming(packet)) => {
                debug!("Received MQTT packet: {:?}", packet);
            }
            Ok(Event::Outgoing(_)) => {
                // Ignore outgoing events
            }
            Err(e) => {
                warn!("MQTT connection error: {}", e);
                tokio::time::sleep(Duration::from_secs(1)).await;
            }
        }
    }
}

pub fn build_topic(domain_id: &str, channel_id: &str, path: &str) -> String {
    format!("m/{}/c/{}/messages/{}", domain_id, channel_id, path)
}
