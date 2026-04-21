use propeller_plugin_sdk::{
    AuthorizeRequest, AuthorizeResponse, EnrichRequest, EnrichResponse, Plugin,
    register_plugin,
};

#[derive(Default)]
struct AuthPlugin;

impl Plugin for AuthPlugin {
    fn authorize(&self, req: AuthorizeRequest) -> AuthorizeResponse {
        if req.task.name.is_empty() {
            return AuthorizeResponse {
                allow: false,
                reason: Some("task name is required".into()),
            };
        }
        if req.context.action == "create" && req.context.user_id.is_empty() {
            return AuthorizeResponse {
                allow: false,
                reason: Some("user_id is required to create tasks".into()),
            };
        }
        AuthorizeResponse { allow: true, reason: None }
    }

    fn enrich(&self, req: EnrichRequest) -> EnrichResponse {
        let mut env = req.task.env.clone();
        env.insert("PROPELLER_PLUGIN_AUTHZ".into(), "plugin-auth".into());
        EnrichResponse { env, ..Default::default() }
    }
}

register_plugin!(AuthPlugin);
