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

        let user_id = &req.context.user_id;

        match req.context.action.as_str() {
            "create" => {
                if user_id.is_empty() {
                    return AuthorizeResponse {
                        allow: false,
                        reason: Some("user_id is required to create tasks".into()),
                    };
                }
            }
            "start" => {
                if user_id.is_empty() {
                    return AuthorizeResponse {
                        allow: false,
                        reason: Some("user_id is required to start tasks".into()),
                    };
                }
                let owner = req.task.env.get("PROPELLER_CREATED_BY").map(String::as_str).unwrap_or("");
                if !owner.is_empty() && owner != user_id {
                    return AuthorizeResponse {
                        allow: false,
                        reason: Some(format!("task was created by '{owner}', only they can start it")),
                    };
                }
            }
            _ => {}
        }

        AuthorizeResponse { allow: true, reason: None }
    }

    fn enrich(&self, req: EnrichRequest) -> EnrichResponse {
        let mut env = req.task.env.clone();
        env.insert("PROPELLER_PLUGIN_AUTHZ".into(), "plugin-auth".into());
        if !req.context.user_id.is_empty() {
            env.insert("PROPELLER_CREATED_BY".into(), req.context.user_id.clone());
        }
        EnrichResponse { env, ..Default::default() }
    }
}

register_plugin!(AuthPlugin);
