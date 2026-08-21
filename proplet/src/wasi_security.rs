use std::{collections::HashMap, net::SocketAddr};

use anyhow::{anyhow, Context, Result};
use serde::Deserialize;
use wasmtime_wasi::sockets::SocketAddrUse;

#[derive(Default, Debug, Clone)]
pub struct WasiSecurity {
    pub env: Option<HashMap<String, String>>,
    pub arguments: Option<Vec<String>>,
    pub storage_readonly: Vec<(String, String)>,
    pub storage_mount: Vec<(String, String)>,
    pub network_bind: Vec<NetworkRule>,
    pub network_connect: Vec<NetworkRule>,
    pub allow_ip_name_lookup: bool,
}

#[derive(Debug, Clone)]
pub struct NetworkRule {
    pub socket: SocketAddr,
    pub protocol: NetworkRuleProtocol,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NetworkRuleProtocol {
    Tcp,
    Udp,
    Both,
}

impl NetworkRule {
    fn matches(&self, protocol: NetworkRuleProtocol, addr: SocketAddr) -> bool {
        if self.protocol != NetworkRuleProtocol::Both && protocol != self.protocol {
            return false;
        }
        if !self.socket.ip().is_unspecified() && self.socket.ip() != addr.ip() {
            return false;
        }
        self.socket.port() == 0 || self.socket.port() == addr.port()
    }

    fn covers(&self, protocol: NetworkRuleProtocol) -> bool {
        self.protocol == NetworkRuleProtocol::Both || self.protocol == protocol
    }
}

impl WasiSecurity {
    /// Parse a policy from its TOML document.
    pub fn from_toml(raw: &str) -> Result<Self> {
        let policy: ParsePolicyFile =
            toml::from_str(raw).context("wasi_security policy is not valid TOML")?;
        WasiSecurity::try_from(policy).map_err(|e| anyhow!("invalid wasi_security policy: {e}"))
    }

    pub fn allows_socket(&self, addr: SocketAddr, use_: SocketAddrUse) -> bool {
        let (rules, protocol) = match use_ {
            SocketAddrUse::TcpBind => (&self.network_bind, NetworkRuleProtocol::Tcp),
            SocketAddrUse::UdpBind => (&self.network_bind, NetworkRuleProtocol::Udp),
            SocketAddrUse::TcpConnect => (&self.network_connect, NetworkRuleProtocol::Tcp),
            SocketAddrUse::UdpConnect | SocketAddrUse::UdpOutgoingDatagram => {
                (&self.network_connect, NetworkRuleProtocol::Udp)
            }
        };

        rules.iter().any(|rule| rule.matches(protocol, addr))
    }

    pub fn uses_tcp(&self) -> bool {
        self.covers_protocol(NetworkRuleProtocol::Tcp)
    }

    pub fn uses_udp(&self) -> bool {
        self.covers_protocol(NetworkRuleProtocol::Udp)
    }

    fn covers_protocol(&self, protocol: NetworkRuleProtocol) -> bool {
        self.network_bind
            .iter()
            .chain(self.network_connect.iter())
            .any(|rule| rule.covers(protocol))
    }

    pub fn has_network_rules(&self) -> bool {
        !self.network_bind.is_empty() || !self.network_connect.is_empty()
    }
}

/// TOML representation of a WASI security policy.
#[derive(Deserialize, Debug, Default)]
struct ParsePolicyFile {
    env: Option<HashMap<String, String>>,
    arguments: Option<Vec<String>>,
    storage: Option<ParsePolicyStorageOptions>,
    network: Option<ParsePolicyNetworkOptions>,
}

#[derive(Deserialize, Debug, Default)]
struct ParsePolicyStorageOptions {
    readonly: Option<Vec<String>>,
    mount: Option<Vec<String>>,
}

#[derive(Deserialize, Debug, Default)]
struct ParsePolicyNetworkOptions {
    allow_ip_name_lookup: Option<bool>,
    bind: Option<Vec<String>>,
    connect: Option<Vec<String>>,
}

impl TryFrom<ParsePolicyFile> for WasiSecurity {
    type Error = String;

    fn try_from(policy: ParsePolicyFile) -> Result<Self, Self::Error> {
        let storage = policy.storage.unwrap_or_default();
        let network = policy.network.unwrap_or_default();

        let storage_readonly = storage
            .readonly
            .unwrap_or_default()
            .iter()
            .map(|entry| parse_storage_entry(entry))
            .collect();
        let storage_mount = storage
            .mount
            .unwrap_or_default()
            .iter()
            .map(|entry| parse_storage_entry(entry))
            .collect();

        let network_bind = network
            .bind
            .unwrap_or_default()
            .iter()
            .map(|rule| parse_network_rule(rule))
            .collect::<Result<Vec<_>, _>>()?;
        let network_connect = network
            .connect
            .unwrap_or_default()
            .iter()
            .map(|rule| parse_network_rule(rule))
            .collect::<Result<Vec<_>, _>>()?;

        Ok(WasiSecurity {
            env: policy.env,
            arguments: policy.arguments,
            storage_readonly,
            storage_mount,
            network_bind,
            network_connect,
            allow_ip_name_lookup: network.allow_ip_name_lookup.unwrap_or(false),
        })
    }
}

/// Parse a storage entry of the form `host::guest`.
fn parse_storage_entry(raw: &str) -> (String, String) {
    match raw.split_once("::") {
        Some((host, guest)) => (host.to_string(), guest.to_string()),
        None => (raw.to_string(), raw.to_string()),
    }
}

/// Parse a network rule of the form `[tcp://|udp://]<ip>:<port>`.
fn parse_network_rule(raw: &str) -> Result<NetworkRule, String> {
    let (protocol, addr) = match raw.split_once("://") {
        Some(("tcp", rest)) => (NetworkRuleProtocol::Tcp, rest),
        Some(("udp", rest)) => (NetworkRuleProtocol::Udp, rest),
        Some((scheme, _)) => return Err(format!("unknown network protocol '{scheme}'")),
        None => (NetworkRuleProtocol::Both, raw),
    };

    let socket = addr
        .parse::<SocketAddr>()
        .map_err(|e| format!("invalid socket address '{addr}': {e}"))?;

    Ok(NetworkRule { socket, protocol })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(toml: &str) -> WasiSecurity {
        WasiSecurity::from_toml(toml).unwrap()
    }

    #[test]
    fn parses_documented_example_policy() {
        let path = concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../examples/wasi-security/policy.toml"
        );
        let doc = std::fs::read_to_string(path).expect("example policy should exist");

        let security = parse(&doc);

        assert_eq!(security.arguments.as_ref().unwrap(), &["--verbose"]);
        assert_eq!(
            security.env.as_ref().unwrap().get("LOG_LEVEL"),
            Some(&"debug".to_string())
        );
        assert_eq!(security.storage_readonly.len(), 1);
        assert_eq!(security.storage_mount.len(), 1);
        assert_eq!(security.network_bind.len(), 1);
        assert_eq!(security.network_connect.len(), 1);
        assert!(!security.allow_ip_name_lookup);
    }

    #[test]
    fn parses_empty_policy() {
        let security = parse("");
        assert!(security.env.is_none());
        assert!(security.arguments.is_none());
        assert!(security.storage_readonly.is_empty());
        assert!(security.storage_mount.is_empty());
        assert!(security.network_bind.is_empty());
        assert!(security.network_connect.is_empty());
        assert!(!security.allow_ip_name_lookup);
    }

    #[test]
    fn parses_env_and_arguments() {
        let security = parse(
            r#"
            arguments = ["--flag", "value"]

            [env]
            KEY = "value"
            "#,
        );

        assert_eq!(
            security.env.as_ref().unwrap().get("KEY"),
            Some(&"value".to_string())
        );
        assert_eq!(security.arguments.unwrap(), vec!["--flag", "value"]);
    }

    #[test]
    fn parses_storage_entries() {
        let security = parse(
            r#"
            [storage]
            readonly = ["/host/ro::/guest/ro", "/shared"]
            mount = ["/host/rw::/guest/rw"]
            "#,
        );

        assert_eq!(
            security.storage_readonly,
            vec![
                ("/host/ro".to_string(), "/guest/ro".to_string()),
                ("/shared".to_string(), "/shared".to_string()),
            ]
        );
        assert_eq!(
            security.storage_mount,
            vec![("/host/rw".to_string(), "/guest/rw".to_string())]
        );
    }

    #[test]
    fn parses_network_rules() {
        let security = parse(
            r#"
            [network]
            allow_ip_name_lookup = true
            bind = ["tcp://0.0.0.0:8080"]
            connect = ["udp://127.0.0.1:53", "10.0.0.1:9000"]
            "#,
        );

        assert!(security.allow_ip_name_lookup);

        assert_eq!(security.network_bind.len(), 1);
        assert_eq!(security.network_bind[0].protocol, NetworkRuleProtocol::Tcp);
        assert_eq!(security.network_bind[0].socket.port(), 8080);

        assert_eq!(security.network_connect.len(), 2);
        assert_eq!(
            security.network_connect[0].protocol,
            NetworkRuleProtocol::Udp
        );
        assert_eq!(
            security.network_connect[1].protocol,
            NetworkRuleProtocol::Both
        );
    }

    #[test]
    fn rejects_invalid_socket_address() {
        let result = WasiSecurity::from_toml(
            r#"
            [network]
            bind = ["tcp://not-an-address"]
            "#,
        );
        assert!(result.is_err());
    }

    #[test]
    fn rejects_unknown_protocol() {
        let result = WasiSecurity::from_toml(
            r#"
            [network]
            connect = ["icmp://127.0.0.1:0"]
            "#,
        );
        assert!(result.is_err());
    }

    #[test]
    fn rejects_invalid_toml() {
        let result = WasiSecurity::from_toml("this is = not valid = toml");
        assert!(result.is_err());
    }

    #[test]
    fn network_rule_matches_respects_protocol() {
        let rule = NetworkRule {
            socket: "0.0.0.0:0".parse().unwrap(),
            protocol: NetworkRuleProtocol::Tcp,
        };
        let addr: SocketAddr = "127.0.0.1:8080".parse().unwrap();

        assert!(rule.matches(NetworkRuleProtocol::Tcp, addr));
        assert!(!rule.matches(NetworkRuleProtocol::Udp, addr));
    }

    #[test]
    fn empty_policy_denies_every_socket_use() {
        let security = parse("");
        let addr: SocketAddr = "127.0.0.1:8080".parse().unwrap();

        for use_ in [
            SocketAddrUse::TcpBind,
            SocketAddrUse::TcpConnect,
            SocketAddrUse::UdpBind,
            SocketAddrUse::UdpConnect,
            SocketAddrUse::UdpOutgoingDatagram,
        ] {
            assert!(!security.allows_socket(addr, use_));
        }

        assert!(!security.uses_tcp());
        assert!(!security.uses_udp());
        assert!(!security.has_network_rules());
    }

    #[test]
    fn allows_socket_separates_bind_from_connect() {
        let security = parse(
            r#"
            [network]
            bind = ["tcp://0.0.0.0:8080"]
            connect = ["tcp://10.0.0.1:9000"]
            "#,
        );

        let bind_addr: SocketAddr = "127.0.0.1:8080".parse().unwrap();
        let connect_addr: SocketAddr = "10.0.0.1:9000".parse().unwrap();

        assert!(security.allows_socket(bind_addr, SocketAddrUse::TcpBind));
        assert!(!security.allows_socket(bind_addr, SocketAddrUse::TcpConnect));

        assert!(security.allows_socket(connect_addr, SocketAddrUse::TcpConnect));
        assert!(!security.allows_socket(connect_addr, SocketAddrUse::TcpBind));

        assert!(security.uses_tcp());
        assert!(!security.uses_udp());
        assert!(security.has_network_rules());
    }

    #[test]
    fn allows_socket_honours_wildcards_and_both_protocol() {
        let security = parse(
            r#"
            [network]
            connect = ["0.0.0.0:53"]
            "#,
        );

        // Unspecified IP matches any host, the port still has to line up.
        assert!(security.allows_socket("8.8.8.8:53".parse().unwrap(), SocketAddrUse::UdpConnect));
        assert!(security.allows_socket("1.1.1.1:53".parse().unwrap(), SocketAddrUse::TcpConnect));
        assert!(!security.allows_socket("8.8.8.8:54".parse().unwrap(), SocketAddrUse::TcpConnect));

        assert!(security.uses_tcp());
        assert!(security.uses_udp());
    }

    #[test]
    fn allows_socket_treats_outgoing_datagram_as_udp_connect() {
        let security = parse(
            r#"
            [network]
            connect = ["udp://127.0.0.1:0"]
            "#,
        );

        // Port 0 in a rule is a wildcard.
        assert!(security.allows_socket(
            "127.0.0.1:1234".parse().unwrap(),
            SocketAddrUse::UdpOutgoingDatagram
        ));
        assert!(
            !security.allows_socket("127.0.0.1:1234".parse().unwrap(), SocketAddrUse::TcpConnect)
        );
    }
}
