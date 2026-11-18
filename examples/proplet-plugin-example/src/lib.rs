wit_bindgen::generate!({
    path: "../../crates/propeller-proplet-plugin-sdk/wit/proplet-plugin.wit",
    world: "proplet-plugin",
});

use exports::propeller::proplet_plugin::lifecycle::{
    AuthorizeResponse, EnrichResponse, TaskInfo, TaskResult,
};

struct Plugin;

impl exports::propeller::proplet_plugin::lifecycle::Guest for Plugin {
    fn authorize(task: TaskInfo) -> AuthorizeResponse {
        if task.name.is_empty() {
            return AuthorizeResponse {
                allow: false,
                reason: Some("task name is required".into()),
            };
        }
        AuthorizeResponse {
            allow: true,
            reason: None,
        }
    }

    fn enrich(task: TaskInfo) -> EnrichResponse {
        EnrichResponse { env: task.env }
    }

    fn on_task_start(task: TaskInfo) {
        eprintln!("[example-plugin] task starting: {}", task.id);
    }

    fn on_task_complete(result: TaskResult) {
        eprintln!(
            "[example-plugin] task {} finished (success={})",
            result.task_id, result.success
        );
    }
}

export!(Plugin);
