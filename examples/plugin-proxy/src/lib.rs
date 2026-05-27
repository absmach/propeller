use propeller_plugin_sdk::{EnrichRequest, EnrichResponse, Plugin, register_plugin};

const REGISTRY_MIRROR: Option<&str> = option_env!("PROPELLER_REGISTRY_MIRROR");

#[derive(Default)]
struct ProxyPlugin;

impl Plugin for ProxyPlugin {
    fn enrich(&self, req: EnrichRequest) -> EnrichResponse {
        let mirror = match REGISTRY_MIRROR {
            Some(m) if !m.is_empty() => m.trim_end_matches('/'),
            _ => return EnrichResponse::default(),
        };

        if req.task.image_url.is_empty() {
            return EnrichResponse::default();
        }

        EnrichResponse {
            image_url: rewrite_registry(&req.task.image_url, mirror),
            ..Default::default()
        }
    }
}

fn rewrite_registry(image_url: &str, mirror: &str) -> String {
    if image_url.starts_with(mirror) {
        return image_url.to_string();
    }

    if let Some(slash_pos) = image_url.find('/') {
        let host = &image_url[..slash_pos];
        if host.contains('.') || host.contains(':') {
            return format!("{}/{}", mirror, &image_url[slash_pos + 1..]);
        }
    }

    format!("{}/{}", mirror, image_url)
}

register_plugin!(ProxyPlugin);

#[cfg(test)]
mod tests {
    use super::rewrite_registry;

    #[test]
    fn test_rewrite_explicit_registry() {
        assert_eq!(
            rewrite_registry("ghcr.io/org/task:latest", "mirror.corp.com"),
            "mirror.corp.com/org/task:latest",
        );
    }

    #[test]
    fn test_rewrite_no_registry() {
        assert_eq!(
            rewrite_registry("myimage:latest", "mirror.corp.com"),
            "mirror.corp.com/myimage:latest",
        );
    }

    #[test]
    fn test_rewrite_already_mirror() {
        assert_eq!(
            rewrite_registry("mirror.corp.com/org/task:latest", "mirror.corp.com"),
            "mirror.corp.com/org/task:latest",
        );
    }

    #[test]
    fn test_rewrite_port_in_registry() {
        assert_eq!(
            rewrite_registry("localhost:5000/org/task:latest", "mirror.corp.com"),
            "mirror.corp.com/org/task:latest",
        );
    }
}
