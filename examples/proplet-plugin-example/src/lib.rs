use propeller_proplet_plugin_sdk::{
    AuthorizeResponse, EnrichResponse, Plugin, TaskInfo, TaskResult,
    register_plugin,
};

/// Example proplet plugin: rejects tasks with no name and stamps an env var.
#[derive(Default)]
struct ExamplePlugin;

impl Plugin for ExamplePlugin {
    fn authorize(&self, task: TaskInfo) -> AuthorizeResponse {
        if task.name.is_empty() {
            return AuthorizeResponse {
                allow: false,
                reason: Some("task name is required".into()),
            };
        }
        AuthorizeResponse { allow: true, reason: None }
    }

    fn enrich(&self, task: TaskInfo) -> EnrichResponse {
        let mut env = task.env.clone();
        env.push(("PROPLET_PLUGIN".into(), "example".into()));
        EnrichResponse { env }
    }

    fn on_task_start(&self, task: TaskInfo) {
        // Logging here goes to the plugin's WASI stderr, visible in proplet logs.
        eprintln!("[example-plugin] task starting: {}", task.id);
    }

    fn on_task_complete(&self, result: TaskResult) {
        eprintln!(
            "[example-plugin] task {} finished (success={})",
            result.task_id, result.success
        );
    }
}

register_plugin!(ExamplePlugin);
