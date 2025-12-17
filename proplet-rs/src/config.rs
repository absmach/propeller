use rumqttc::QoS;
use serde::Deserialize;
use std::env;
use std::time::Duration;
use uuid::Uuid;

#[derive(Debug, Clone, Deserialize)]
#[allow(dead_code)]
pub struct Config {
    pub proplet: PropletConfig,
}

#[derive(Debug, Clone, Deserialize)]
pub struct PropletConfig {
    pub log_level: String,
    pub instance_id: Uuid,
    pub mqtt_address: String,
    pub mqtt_timeout: u64,
    pub mqtt_qos: u8,
    pub mqtt_keep_alive: u64,
    pub mqtt_max_packet_size: usize,
    pub mqtt_inflight: u16,
    pub mqtt_request_channel_capacity: usize,
    pub liveliness_interval: u64,
    pub domain_id: String,
    pub channel_id: String,
    pub client_id: String,
    pub client_key: String,
    pub k8s_namespace: Option<String>,
    pub external_wasm_runtime: Option<String>,
}

impl Default for PropletConfig {
    fn default() -> Self {
        Self {
            log_level: "info".to_string(),
            instance_id: Uuid::new_v4(),
            mqtt_address: "tcp://localhost:1883".to_string(),
            mqtt_timeout: 30,
            mqtt_qos: 2,
            mqtt_keep_alive: 30,
            mqtt_max_packet_size: 10 * 1024 * 1024, // 10MB
            mqtt_inflight: 10,
            mqtt_request_channel_capacity: 128,
            liveliness_interval: 10,
            domain_id: String::new(),
            channel_id: String::new(),
            client_id: String::new(),
            client_key: String::new(),
            k8s_namespace: None,
            external_wasm_runtime: None,
        }
    }
}

impl PropletConfig {
    pub fn from_env() -> Self {
        let mut config = Self::default();

        if let Ok(val) = env::var("PROPLET_LOG_LEVEL") {
            config.log_level = val;
        }

        if let Ok(val) = env::var("PROPLET_INSTANCE_ID") {
            if let Ok(uuid) = Uuid::parse_str(&val) {
                config.instance_id = uuid;
            }
        }

        if let Ok(val) = env::var("PROPLET_MQTT_ADDRESS") {
            config.mqtt_address = val;
        }

        if let Ok(val) = env::var("PROPLET_MQTT_TIMEOUT") {
            if let Ok(timeout) = val.parse() {
                config.mqtt_timeout = timeout;
            }
        }

        if let Ok(val) = env::var("PROPLET_MQTT_QOS") {
            if let Ok(qos) = val.parse() {
                config.mqtt_qos = qos;
            }
        }

        if let Ok(val) = env::var("PROPLET_MQTT_KEEP_ALIVE") {
            if let Ok(keep_alive) = val.parse() {
                config.mqtt_keep_alive = keep_alive;
            }
        }

        if let Ok(val) = env::var("PROPLET_MQTT_MAX_PACKET_SIZE") {
            if let Ok(max_packet_size) = val.parse() {
                config.mqtt_max_packet_size = max_packet_size;
            }
        }

        if let Ok(val) = env::var("PROPLET_MQTT_INFLIGHT") {
            if let Ok(inflight) = val.parse() {
                config.mqtt_inflight = inflight;
            }
        }

        if let Ok(val) = env::var("PROPLET_MQTT_REQUEST_CHANNEL_CAPACITY") {
            if let Ok(capacity) = val.parse() {
                config.mqtt_request_channel_capacity = capacity;
            }
        }

        if let Ok(val) = env::var("PROPLET_LIVELINESS_INTERVAL") {
            if let Ok(interval) = val.parse() {
                config.liveliness_interval = interval;
            }
        }

        if let Ok(val) = env::var("PROPLET_DOMAIN_ID") {
            config.domain_id = val;
        }

        if let Ok(val) = env::var("PROPLET_CHANNEL_ID") {
            config.channel_id = val;
        }

        if let Ok(val) = env::var("PROPLET_CLIENT_ID") {
            config.client_id = val;
        }

        if let Ok(val) = env::var("PROPLET_CLIENT_KEY") {
            config.client_key = val;
        }

        if let Ok(val) = env::var("PROPLET_MANAGER_K8S_NAMESPACE") {
            config.k8s_namespace = Some(val);
        }

        if let Ok(val) = env::var("PROPLET_EXTERNAL_WASM_RUNTIME") {
            config.external_wasm_runtime = Some(val);
        }

        config
    }

    pub fn qos(&self) -> QoS {
        match self.mqtt_qos {
            0 => QoS::AtMostOnce,
            1 => QoS::AtLeastOnce,
            _ => QoS::ExactlyOnce,
        }
    }

    pub fn mqtt_timeout(&self) -> Duration {
        Duration::from_secs(self.mqtt_timeout)
    }

    pub fn mqtt_keep_alive(&self) -> Duration {
        Duration::from_secs(self.mqtt_keep_alive)
    }

    pub fn liveliness_interval(&self) -> Duration {
        Duration::from_secs(self.liveliness_interval)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::env;

    #[test]
    fn test_proplet_config_default() {
        let config = PropletConfig::default();

        assert_eq!(config.log_level, "info");
        assert_eq!(config.mqtt_address, "tcp://localhost:1883");
        assert_eq!(config.mqtt_timeout, 30);
        assert_eq!(config.mqtt_qos, 2);
        assert_eq!(config.liveliness_interval, 10);
        assert!(config.domain_id.is_empty());
        assert!(config.channel_id.is_empty());
        assert!(config.client_id.is_empty());
        assert!(config.client_key.is_empty());
        assert!(config.k8s_namespace.is_none());
        assert!(config.external_wasm_runtime.is_none());
    }

    #[test]
    fn test_proplet_config_qos_at_most_once() {
        let mut config = PropletConfig::default();
        config.mqtt_qos = 0;

        assert!(matches!(config.qos(), QoS::AtMostOnce));
    }

    #[test]
    fn test_proplet_config_qos_at_least_once() {
        let mut config = PropletConfig::default();
        config.mqtt_qos = 1;

        assert!(matches!(config.qos(), QoS::AtLeastOnce));
    }

    #[test]
    fn test_proplet_config_qos_exactly_once() {
        let mut config = PropletConfig::default();
        config.mqtt_qos = 2;

        assert!(matches!(config.qos(), QoS::ExactlyOnce));
    }

    #[test]
    fn test_proplet_config_qos_invalid_defaults_to_exactly_once() {
        let mut config = PropletConfig::default();
        config.mqtt_qos = 99;

        assert!(matches!(config.qos(), QoS::ExactlyOnce));
    }

    #[test]
    fn test_proplet_config_mqtt_timeout() {
        let mut config = PropletConfig::default();
        config.mqtt_timeout = 60;

        assert_eq!(config.mqtt_timeout(), Duration::from_secs(60));
    }

    #[test]
    fn test_proplet_config_liveliness_interval() {
        let mut config = PropletConfig::default();
        config.liveliness_interval = 30;

        assert_eq!(config.liveliness_interval(), Duration::from_secs(30));
    }

    #[test]
    fn test_proplet_config_from_env_log_level() {
        env::set_var("PROPLET_LOG_LEVEL", "debug");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_LOG_LEVEL");

        assert_eq!(config.log_level, "debug");
    }

    #[test]
    fn test_proplet_config_from_env_mqtt_address() {
        env::set_var("PROPLET_MQTT_ADDRESS", "tcp://mqtt.example.com:1883");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_MQTT_ADDRESS");

        assert_eq!(config.mqtt_address, "tcp://mqtt.example.com:1883");
    }

    #[test]
    fn test_proplet_config_from_env_mqtt_timeout() {
        env::set_var("PROPLET_MQTT_TIMEOUT", "120");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_MQTT_TIMEOUT");

        assert_eq!(config.mqtt_timeout, 120);
    }

    #[test]
    fn test_proplet_config_from_env_mqtt_qos() {
        env::set_var("PROPLET_MQTT_QOS", "1");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_MQTT_QOS");

        assert_eq!(config.mqtt_qos, 1);
    }

    #[test]
    fn test_proplet_config_from_env_liveliness_interval() {
        env::set_var("PROPLET_LIVELINESS_INTERVAL", "20");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_LIVELINESS_INTERVAL");

        assert_eq!(config.liveliness_interval, 20);
    }

    #[test]
    fn test_proplet_config_from_env_domain_id() {
        env::set_var("PROPLET_DOMAIN_ID", "domain-123");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_DOMAIN_ID");

        assert_eq!(config.domain_id, "domain-123");
    }

    #[test]
    fn test_proplet_config_from_env_channel_id() {
        env::set_var("PROPLET_CHANNEL_ID", "channel-456");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_CHANNEL_ID");

        assert_eq!(config.channel_id, "channel-456");
    }

    #[test]
    fn test_proplet_config_from_env_client_id() {
        env::set_var("PROPLET_CLIENT_ID", "client-789");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_CLIENT_ID");

        assert_eq!(config.client_id, "client-789");
    }

    #[test]
    fn test_proplet_config_from_env_client_key() {
        env::set_var("PROPLET_CLIENT_KEY", "secret-key");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_CLIENT_KEY");

        assert_eq!(config.client_key, "secret-key");
    }

    #[test]
    fn test_proplet_config_from_env_k8s_namespace() {
        env::set_var("PROPLET_MANAGER_K8S_NAMESPACE", "production");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_MANAGER_K8S_NAMESPACE");

        assert_eq!(config.k8s_namespace, Some("production".to_string()));
    }

    #[test]
    fn test_proplet_config_from_env_external_runtime() {
        env::set_var("PROPLET_EXTERNAL_WASM_RUNTIME", "/usr/local/bin/wasmtime");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_EXTERNAL_WASM_RUNTIME");

        assert_eq!(
            config.external_wasm_runtime,
            Some("/usr/local/bin/wasmtime".to_string())
        );
    }

    #[test]
    fn test_proplet_config_from_env_instance_id_valid() {
        let uuid = Uuid::new_v4();
        env::set_var("PROPLET_INSTANCE_ID", uuid.to_string());
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_INSTANCE_ID");

        assert_eq!(config.instance_id, uuid);
    }

    #[test]
    fn test_proplet_config_from_env_instance_id_invalid() {
        env::set_var("PROPLET_INSTANCE_ID", "not-a-valid-uuid");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_INSTANCE_ID");

        // Should keep default UUID when invalid
        assert_ne!(config.instance_id.to_string(), "not-a-valid-uuid");
    }

    #[test]
    fn test_proplet_config_from_env_mqtt_timeout_invalid() {
        env::set_var("PROPLET_MQTT_TIMEOUT", "not-a-number");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_MQTT_TIMEOUT");

        assert_eq!(config.mqtt_timeout, 30);
    }

    #[test]
    fn test_proplet_config_from_env_mqtt_qos_invalid() {
        env::set_var("PROPLET_MQTT_QOS", "invalid");
        let config = PropletConfig::from_env();
        env::remove_var("PROPLET_MQTT_QOS");

        assert_eq!(config.mqtt_qos, 2);
    }

    #[test]
    fn test_proplet_config_from_env_no_env_vars() {
        let vars_to_clear = vec![
            "PROPLET_LOG_LEVEL",
            "PROPLET_MQTT_ADDRESS",
            "PROPLET_MQTT_TIMEOUT",
            "PROPLET_MQTT_QOS",
            "PROPLET_LIVELINESS_INTERVAL",
            "PROPLET_DOMAIN_ID",
            "PROPLET_CHANNEL_ID",
            "PROPLET_CLIENT_ID",
            "PROPLET_CLIENT_KEY",
            "PROPLET_MANAGER_K8S_NAMESPACE",
            "PROPLET_EXTERNAL_WASM_RUNTIME",
        ];

        for var in &vars_to_clear {
            env::remove_var(var);
        }

        let config = PropletConfig::from_env();

        assert_eq!(config.log_level, "info");
        assert_eq!(config.mqtt_address, "tcp://localhost:1883");
        assert_eq!(config.mqtt_timeout, 30);
    }

    #[test]
    fn test_proplet_config_timeout_conversion() {
        let config = PropletConfig {
            mqtt_timeout: 45,
            ..PropletConfig::default()
        };

        let timeout = config.mqtt_timeout();
        assert_eq!(timeout.as_secs(), 45);
    }

    #[test]
    fn test_proplet_config_liveliness_interval_conversion() {
        let config = PropletConfig {
            liveliness_interval: 15,
            ..PropletConfig::default()
        };

        let interval = config.liveliness_interval();
        assert_eq!(interval.as_secs(), 15);
    }
}
