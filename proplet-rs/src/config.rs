use rumqttc::QoS;
use serde::Deserialize;
use std::env;
use std::time::Duration;
use uuid::Uuid;

#[derive(Debug, Clone, Deserialize)]
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

    pub fn liveliness_interval(&self) -> Duration {
        Duration::from_secs(self.liveliness_interval)
    }
}
