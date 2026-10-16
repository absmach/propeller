use anyhow::{anyhow, Context, Result};
use rumqttc::{AsyncClient, Event, EventLoop, MqttOptions, Packet, QoS};
use serde::{de::DeserializeOwned, Serialize};
use std::time::Duration;
use tokio::sync::mpsc;
use tracing::{debug, error, info, warn};
use url::Url;

pub struct MqttConfig {
    pub address: String,
    pub entity_id: String,
    #[allow(dead_code)]
    pub timeout: Duration,
    #[allow(dead_code)]
    pub qos: QoS,
    pub keep_alive: Duration,
    pub max_packet_size: usize,
    pub inflight: u16,
    pub request_channel_capacity: usize,
    pub username: String,
    pub password: String,
    pub tls_ca_cert: Option<String>,
    pub tls_client_cert: Option<String>,
    pub tls_client_key: Option<String>,
    pub tls_insecure_skip_verify: bool,
}

#[derive(Clone)]
pub struct PubSub {
    client: AsyncClient,
}

impl PubSub {
    pub async fn new(config: MqttConfig) -> Result<(Self, EventLoop)> {
        let (host, port, use_tls) = parse_mqtt_address(&config.address)?;

        let mut mqtt_options = MqttOptions::new(config.entity_id, host, port);

        mqtt_options.set_keep_alive(config.keep_alive);
        mqtt_options.set_credentials(config.username, config.password);
        mqtt_options.set_max_packet_size(config.max_packet_size, config.max_packet_size);
        mqtt_options.set_inflight(config.inflight);
        mqtt_options.set_clean_session(true);

        if use_tls {
            let transport = if let Some(ca_pem) = &config.tls_ca_cert {
                let ca = std::fs::read(ca_pem)
                    .with_context(|| format!("Failed to read CA certificate: {ca_pem}"))?;

                let client_auth = match (&config.tls_client_cert, &config.tls_client_key) {
                    (Some(cert_path), Some(key_path)) => {
                        let cert = std::fs::read(cert_path)
                            .with_context(|| format!("Failed to read client cert: {cert_path}"))?;
                        let key = std::fs::read(key_path)
                            .with_context(|| format!("Failed to read client key: {key_path}"))?;
                        Some((cert, key))
                    }
                    (Some(_), None) | (None, Some(_)) => {
                        warn!("mTLS requires both PROPLET_MQTT_TLS_CLIENT_CERT and PROPLET_MQTT_TLS_CLIENT_KEY — client auth disabled");
                        None
                    }
                    _ => None,
                };

                // Note: PROPLET_MQTT_TLS_INSECURE_SKIP_VERIFY is not supported by the
                // rumqttc::Transport::tls() API and has no effect when a CA cert is provided.
                // Use the Go manager/proxy if insecure TLS is required.
                if config.tls_insecure_skip_verify {
                    warn!("PROPLET_MQTT_TLS_INSECURE_SKIP_VERIFY has no effect when PROPLET_MQTT_TLS_CA_CERT is set — certificate verification is always enforced");
                }
                rumqttc::Transport::tls(ca, client_auth, None)
            } else {
                if config.tls_client_cert.is_some() || config.tls_client_key.is_some() {
                    warn!("PROPLET_MQTT_TLS_CLIENT_CERT/KEY are set but ignored — mTLS requires PROPLET_MQTT_TLS_CA_CERT");
                }
                if config.tls_insecure_skip_verify {
                    warn!("PROPLET_MQTT_TLS_INSECURE_SKIP_VERIFY is not supported by the Rust proplet — certificate verification uses system root CAs");
                }
                rumqttc::Transport::tls_with_default_config()
            };
            mqtt_options.set_transport(transport);
        }

        let (client, eventloop) = AsyncClient::new(mqtt_options, config.request_channel_capacity);

        info!("MQTT client created (TLS: {})", use_tls);

        Ok((Self { client }, eventloop))
    }

    pub async fn publish<T: Serialize>(&self, topic: &str, payload: &T, qos: QoS) -> Result<()> {
        let json = serde_json::to_vec(payload).context("Failed to serialize payload")?;

        self.client
            .publish(topic, qos, false, json)
            .await
            .context("Failed to publish message")?;

        debug!("Published to topic: {}", topic);
        Ok(())
    }

    pub async fn subscribe(&self, topic: &str, qos: QoS) -> Result<()> {
        self.client
            .subscribe(topic, qos)
            .await
            .context(format!("Failed to subscribe to topic: {topic}"))?;

        info!("Subscribed to topic: {}", topic);
        Ok(())
    }

    #[allow(dead_code)]
    pub async fn unsubscribe(&self, topic: &str) -> Result<()> {
        self.client
            .unsubscribe(topic)
            .await
            .context(format!("Failed to unsubscribe from topic: {topic}"))?;

        info!("Unsubscribed from topic: {}", topic);
        Ok(())
    }

    pub async fn disconnect(&self) -> Result<()> {
        info!("Sending MQTT DISCONNECT packet");
        self.client
            .disconnect()
            .await
            .context("Failed to send DISCONNECT packet")?;
        info!("MQTT client disconnected gracefully");
        Ok(())
    }
}

pub struct MqttMessage {
    pub topic: String,
    pub payload: Vec<u8>,
    pub is_reconnect: bool,
}

impl MqttMessage {
    pub fn decode<T: DeserializeOwned>(&self) -> Result<T> {
        let value: serde_json::Value = serde_json::from_slice(&self.payload).context(format!(
            "Failed to deserialize message payload from topic '{}'",
            self.topic
        ))?;

        let envelope = crate::jsonrpc::unwrap(value);

        serde_json::from_value(envelope.payload).context(format!(
            "Failed to deserialize message payload from topic '{}'",
            self.topic
        ))
    }

    pub fn correlation_id(&self) -> Option<serde_json::Value> {
        let value: serde_json::Value = serde_json::from_slice(&self.payload).ok()?;

        crate::jsonrpc::unwrap(value).id
    }

    pub fn envelope_method(&self) -> Option<String> {
        let value: serde_json::Value = serde_json::from_slice(&self.payload).ok()?;

        crate::jsonrpc::unwrap(value).method
    }
}

pub async fn process_mqtt_events(mut eventloop: EventLoop, tx: mpsc::Sender<MqttMessage>) {
    info!("Starting MQTT event loop");

    let mut backoff_secs: u64 = 1;

    loop {
        match eventloop.poll().await {
            Ok(Event::Incoming(Packet::Publish(publish))) => {
                let msg = MqttMessage {
                    topic: publish.topic.clone(),
                    payload: publish.payload.to_vec(),
                    is_reconnect: false,
                };

                if let Err(e) = tx.send(msg).await {
                    error!("Failed to send MQTT message to handler: {}", e);
                }
            }
            Ok(Event::Incoming(Packet::ConnAck(connack))) => {
                debug!("Received MQTT packet: ConnAck({:?})", connack);
                // If session_present is false, subscriptions are lost and need to be re-established
                if !connack.session_present {
                    info!("MQTT session not present, triggering re-subscription");
                    let reconnect_msg = MqttMessage {
                        topic: "__reconnect__".to_string(),
                        payload: Vec::new(),
                        is_reconnect: true,
                    };
                    if let Err(e) = tx.send(reconnect_msg).await {
                        error!("Failed to send reconnect notification: {}", e);
                        // Do not reset backoff — re-subscription failed, keep pressure off broker
                        continue;
                    }
                }
                backoff_secs = 1;
            }
            Ok(Event::Incoming(packet)) => {
                debug!("Received MQTT packet: {:?}", packet);
            }
            Ok(Event::Outgoing(_)) => {}
            Err(e) => {
                warn!(
                    "MQTT connection error, retrying in {}s: {}",
                    backoff_secs, e
                );
                tokio::time::sleep(Duration::from_secs(backoff_secs)).await;
                backoff_secs = (backoff_secs * 2).min(60);
            }
        }
    }
}

pub fn build_topic(tenant_id: &str, channel_id: &str, path: &str) -> String {
    format!("m/{tenant_id}/c/{channel_id}/{path}")
}

fn parse_mqtt_address(address: &str) -> Result<(String, u16, bool)> {
    if let Ok(url) = Url::parse(address) {
        let scheme = url.scheme();
        let use_tls = matches!(scheme, "ssl" | "tls" | "mqtts");

        let mut host = url
            .host_str()
            .ok_or_else(|| anyhow!("Missing host in URL: {address}"))?
            .to_string();

        if host.starts_with('[') && host.ends_with(']') {
            host = host[1..host.len() - 1].to_string();
        }

        let default_port = if use_tls { 8883 } else { 1883 };
        let port = url.port().unwrap_or(default_port);

        return Ok((host, port, use_tls));
    }

    if address.starts_with('[') {
        if let Some(bracket_end) = address.find(']') {
            let host = address[1..bracket_end].to_string();
            let port = if let Some(colon_pos) = address[bracket_end..].find(':') {
                address[bracket_end + colon_pos + 1..]
                    .parse::<u16>()
                    .unwrap_or(1883)
            } else {
                1883
            };
            return Ok((host, port, false));
        }
    }

    if let Some(colon_pos) = address.rfind(':') {
        let host = address[..colon_pos].to_string();
        let port = address[colon_pos + 1..]
            .parse::<u16>()
            .context("Invalid port number")?;
        return Ok((host, port, false));
    }

    Ok((address.to_string(), 1883, false))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::StartRequest;

    fn message(payload: serde_json::Value) -> MqttMessage {
        MqttMessage {
            topic: "m/t/c/c/control/manager/start".to_string(),
            payload: serde_json::to_vec(&payload).unwrap(),
            is_reconnect: false,
        }
    }

    #[test]
    fn test_decode_accepts_a_legacy_flat_payload() {
        let msg = message(serde_json::json!({
            "id": "task-1",
            "name": "fn",
            "image_url": "registry.example.com/app:v1",
            "state": 0
        }));

        let req: StartRequest = msg.decode().unwrap();
        assert_eq!(req.id, "task-1");
        assert_eq!(req.name, "fn");
        assert!(msg.correlation_id().is_none());
    }

    #[test]
    fn test_decode_accepts_a_jsonrpc_envelope() {
        let msg = message(serde_json::json!({
            "jsonrpc": "2.0",
            "method": "task.start",
            "params": {
                "id": "task-1",
                "name": "fn",
                "image_url": "registry.example.com/app:v1",
                "state": 0
            },
            "id": "task-1"
        }));

        let req: StartRequest = msg.decode().unwrap();
        assert_eq!(req.id, "task-1");
        assert_eq!(req.name, "fn");
        assert_eq!(msg.correlation_id(), Some(serde_json::json!("task-1")));
    }

    #[test]
    fn test_parse_mqtt_address_tcp_scheme() {
        let (host, port, tls) = parse_mqtt_address("tcp://localhost:1883").unwrap();
        assert_eq!(host, "localhost");
        assert_eq!(port, 1883);
        assert!(!tls);
    }

    #[test]
    fn test_parse_mqtt_address_mqtt_scheme() {
        let (host, port, tls) = parse_mqtt_address("mqtt://broker.example.com:1883").unwrap();
        assert_eq!(host, "broker.example.com");
        assert_eq!(port, 1883);
        assert!(!tls);
    }

    #[test]
    fn test_parse_mqtt_address_ssl_scheme() {
        let (host, port, tls) = parse_mqtt_address("ssl://broker.example.com:8883").unwrap();
        assert_eq!(host, "broker.example.com");
        assert_eq!(port, 8883);
        assert!(tls);
    }

    #[test]
    fn test_parse_mqtt_address_tls_scheme() {
        let (host, port, tls) = parse_mqtt_address("tls://broker.example.com:8883").unwrap();
        assert_eq!(host, "broker.example.com");
        assert_eq!(port, 8883);
        assert!(tls);
    }

    #[test]
    fn test_parse_mqtt_address_mqtts_scheme() {
        let (host, port, tls) = parse_mqtt_address("mqtts://broker.example.com").unwrap();
        assert_eq!(host, "broker.example.com");
        assert_eq!(port, 8883); // Default TLS port
        assert!(tls);
    }

    #[test]
    fn test_parse_mqtt_address_host_port() {
        let (host, port, tls) = parse_mqtt_address("192.168.1.100:1883").unwrap();
        assert_eq!(host, "192.168.1.100");
        assert_eq!(port, 1883);
        assert!(!tls);
    }

    #[test]
    fn test_parse_mqtt_address_ipv6_with_scheme() {
        let (host, port, tls) = parse_mqtt_address("tcp://[::1]:1883").unwrap();
        assert_eq!(host, "::1");
        assert_eq!(port, 1883);
        assert!(!tls);
    }

    #[test]
    fn test_parse_mqtt_address_ipv6_with_tls() {
        let (host, port, tls) = parse_mqtt_address("mqtts://[2001:db8::1]:8883").unwrap();
        assert_eq!(host, "2001:db8::1");
        assert_eq!(port, 8883);
        assert!(tls);
    }

    #[test]
    fn test_parse_mqtt_address_ipv6_no_scheme() {
        let (host, port, tls) = parse_mqtt_address("[::1]:1883").unwrap();
        assert_eq!(host, "::1");
        assert_eq!(port, 1883);
        assert!(!tls);
    }

    #[test]
    fn test_parse_mqtt_address_ipv6_no_port() {
        let (host, port, tls) = parse_mqtt_address("[fe80::1]").unwrap();
        assert_eq!(host, "fe80::1");
        assert_eq!(port, 1883); // Default port
        assert!(!tls);
    }

    #[test]
    fn test_parse_mqtt_address_hostname_only() {
        let (host, port, tls) = parse_mqtt_address("localhost").unwrap();
        assert_eq!(host, "localhost");
        assert_eq!(port, 1883);
        assert!(!tls);
    }

    #[test]
    fn test_parse_mqtt_address_default_port_with_scheme() {
        let (host, port, tls) = parse_mqtt_address("tcp://broker.example.com").unwrap();
        assert_eq!(host, "broker.example.com");
        assert_eq!(port, 1883);
        assert!(!tls);
    }

    #[test]
    fn test_build_topic() {
        let topic = build_topic("tenant-1", "channel-1", "control/manager/start");
        assert_eq!(topic, "m/tenant-1/c/channel-1/control/manager/start");
    }

    #[test]
    fn test_build_topic_with_empty_path() {
        let topic = build_topic("tenant-1", "channel-1", "");
        assert_eq!(topic, "m/tenant-1/c/channel-1/");
    }

    #[test]
    fn test_build_topic_with_slashes() {
        let topic = build_topic("tenant/1", "channel/1", "path/to/topic");
        assert_eq!(topic, "m/tenant/1/c/channel/1/path/to/topic");
    }

    #[test]
    fn test_mqtt_message_decode_success() {
        #[derive(Debug, serde::Deserialize, PartialEq)]
        struct TestPayload {
            id: String,
            value: i32,
        }

        let payload = serde_json::json!({
            "id": "test-123",
            "value": 42
        });

        let msg = MqttMessage {
            topic: "test/topic".to_string(),
            payload: serde_json::to_vec(&payload).unwrap(),
            is_reconnect: false,
        };

        let decoded: TestPayload = msg.decode().unwrap();
        assert_eq!(decoded.id, "test-123");
        assert_eq!(decoded.value, 42);
    }

    #[test]
    fn test_mqtt_message_decode_failure() {
        let msg = MqttMessage {
            topic: "test/topic".to_string(),
            payload: b"invalid json".to_vec(),
            is_reconnect: false,
        };

        let result: Result<serde_json::Value> = msg.decode();
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Failed to deserialize"));
    }

    #[test]
    fn test_mqtt_message_decode_empty_payload() {
        let msg = MqttMessage {
            topic: "test/topic".to_string(),
            payload: Vec::new(),
            is_reconnect: false,
        };

        let result: Result<serde_json::Value> = msg.decode();
        assert!(result.is_err());
    }

    #[test]
    fn test_mqtt_config_address_parsing_with_scheme_and_port() {
        let address = "tcp://broker.example.com:1883";
        let address_without_scheme = address.split("://").nth(1).unwrap_or(address);

        let (host, port) = if let Some(colon_pos) = address_without_scheme.rfind(':') {
            let host = &address_without_scheme[..colon_pos];
            let port = address_without_scheme[colon_pos + 1..]
                .parse::<u16>()
                .unwrap_or(1883);
            (host, port)
        } else {
            (address_without_scheme, 1883)
        };

        assert_eq!(host, "broker.example.com");
        assert_eq!(port, 1883);
    }

    #[test]
    fn test_mqtt_config_address_parsing_without_scheme() {
        let address = "localhost:1883";
        let address_without_scheme = address.split("://").nth(1).unwrap_or(address);

        let (host, port) = if let Some(colon_pos) = address_without_scheme.rfind(':') {
            let host = &address_without_scheme[..colon_pos];
            let port = address_without_scheme[colon_pos + 1..]
                .parse::<u16>()
                .unwrap_or(1883);
            (host, port)
        } else {
            (address_without_scheme, 1883)
        };

        assert_eq!(host, "localhost");
        assert_eq!(port, 1883);
    }

    #[test]
    fn test_mqtt_config_address_parsing_without_port() {
        let address = "tcp://broker.example.com";
        let address_without_scheme = address.split("://").nth(1).unwrap_or(address);

        let (host, port) = if let Some(colon_pos) = address_without_scheme.rfind(':') {
            let host = &address_without_scheme[..colon_pos];
            let port = address_without_scheme[colon_pos + 1..]
                .parse::<u16>()
                .unwrap_or(1883);
            (host, port)
        } else {
            (address_without_scheme, 1883)
        };

        assert_eq!(host, "broker.example.com");
        assert_eq!(port, 1883);
    }

    #[test]
    fn test_mqtt_config_address_parsing_custom_port() {
        let address = "tcp://broker.example.com:8883";
        let address_without_scheme = address.split("://").nth(1).unwrap_or(address);

        let (host, port) = if let Some(colon_pos) = address_without_scheme.rfind(':') {
            let host = &address_without_scheme[..colon_pos];
            let port = address_without_scheme[colon_pos + 1..]
                .parse::<u16>()
                .unwrap_or(1883);
            (host, port)
        } else {
            (address_without_scheme, 1883)
        };

        assert_eq!(host, "broker.example.com");
        assert_eq!(port, 8883);
    }

    #[test]
    fn test_mqtt_config_address_parsing_invalid_port() {
        let address = "tcp://broker.example.com:invalid";
        let address_without_scheme = address.split("://").nth(1).unwrap_or(address);

        let (host, port) = if let Some(colon_pos) = address_without_scheme.rfind(':') {
            let host = &address_without_scheme[..colon_pos];
            let port = address_without_scheme[colon_pos + 1..]
                .parse::<u16>()
                .unwrap_or(1883);
            (host, port)
        } else {
            (address_without_scheme, 1883)
        };

        assert_eq!(host, "broker.example.com");
        assert_eq!(port, 1883); // Falls back to default
    }

    #[test]
    fn test_mqtt_config_address_ipv4() {
        let address = "tcp://192.168.1.100:1883";
        let address_without_scheme = address.split("://").nth(1).unwrap_or(address);

        let (host, port) = if let Some(colon_pos) = address_without_scheme.rfind(':') {
            let host = &address_without_scheme[..colon_pos];
            let port = address_without_scheme[colon_pos + 1..]
                .parse::<u16>()
                .unwrap_or(1883);
            (host, port)
        } else {
            (address_without_scheme, 1883)
        };

        assert_eq!(host, "192.168.1.100");
        assert_eq!(port, 1883);
    }

    #[test]
    fn test_mqtt_config_address_ipv6() {
        let address = "tcp://[::1]:1883";
        let address_without_scheme = address.split("://").nth(1).unwrap_or(address);

        let (host, port) = if let Some(colon_pos) = address_without_scheme.rfind(':') {
            let host = &address_without_scheme[..colon_pos];
            let port = address_without_scheme[colon_pos + 1..]
                .parse::<u16>()
                .unwrap_or(1883);
            (host, port)
        } else {
            (address_without_scheme, 1883)
        };

        assert_eq!(host, "[::1]");
        assert_eq!(port, 1883);
    }

    #[test]
    fn test_mqtt_message_decode_with_nested_structure() {
        #[derive(Debug, serde::Deserialize, PartialEq)]
        struct NestedPayload {
            outer: OuterData,
        }

        #[derive(Debug, serde::Deserialize, PartialEq)]
        struct OuterData {
            inner: InnerData,
        }

        #[derive(Debug, serde::Deserialize, PartialEq)]
        struct InnerData {
            value: String,
        }

        let payload = serde_json::json!({
            "outer": {
                "inner": {
                    "value": "nested-value"
                }
            }
        });

        let msg = MqttMessage {
            topic: "nested/topic".to_string(),
            payload: serde_json::to_vec(&payload).unwrap(),
            is_reconnect: false,
        };

        let decoded: NestedPayload = msg.decode().unwrap();
        assert_eq!(decoded.outer.inner.value, "nested-value");
    }

    #[test]
    fn test_mqtt_config_creation() {
        let config = MqttConfig {
            address: "tcp://localhost:1883".to_string(),
            entity_id: "test-client".to_string(),
            timeout: Duration::from_secs(30),
            qos: QoS::ExactlyOnce,
            username: "user".to_string(),
            password: "pass".to_string(),
            keep_alive: Duration::from_secs(30),
            max_packet_size: 1024,
            inflight: 10,
            request_channel_capacity: 128,
            tls_ca_cert: None,
            tls_client_cert: None,
            tls_client_key: None,
            tls_insecure_skip_verify: false,
        };

        assert_eq!(config.address, "tcp://localhost:1883");
        assert_eq!(config.entity_id, "test-client");
        assert_eq!(config.timeout, Duration::from_secs(30));
        assert_eq!(config.username, "user");
        assert_eq!(config.password, "pass");
        assert!(config.tls_ca_cert.is_none());
        assert!(!config.tls_insecure_skip_verify);
    }

    #[test]
    fn test_mqtt_config_tls_configuration() {
        let config = MqttConfig {
            address: "mqtts://broker:8883".to_string(),
            entity_id: "tls-client".to_string(),
            timeout: Duration::from_secs(30),
            qos: QoS::AtLeastOnce,
            username: "user".to_string(),
            password: "pass".to_string(),
            keep_alive: Duration::from_secs(30),
            max_packet_size: 1024,
            inflight: 10,
            request_channel_capacity: 128,
            tls_ca_cert: Some("/etc/ssl/certs/ca.pem".to_string()),
            tls_client_cert: Some("/etc/ssl/certs/client.pem".to_string()),
            tls_client_key: Some("/etc/ssl/certs/client-key.pem".to_string()),
            tls_insecure_skip_verify: false,
        };

        assert_eq!(
            config.tls_ca_cert,
            Some("/etc/ssl/certs/ca.pem".to_string())
        );
        assert_eq!(
            config.tls_client_cert,
            Some("/etc/ssl/certs/client.pem".to_string())
        );
        assert_eq!(
            config.tls_client_key,
            Some("/etc/ssl/certs/client-key.pem".to_string())
        );
        assert!(!config.tls_insecure_skip_verify);
    }
}
