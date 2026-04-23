wit_bindgen::generate!({
    path: "wit/proplet-plugin.wit",
    world: "proplet-plugin",
});

pub use exports::propeller::proplet_plugin::lifecycle::{
    AuthorizeResponse, EnrichResponse, TaskInfo, TaskResult,
};

/// Implement this trait to define plugin behaviour.
///
/// All methods have default no-op / allow-all implementations, so you only
/// override what you need.
///
/// ```ignore
/// #[derive(Default)]
/// struct MyPlugin;
///
/// impl Plugin for MyPlugin {
///     fn authorize(&self, task: TaskInfo) -> AuthorizeResponse {
///         if task.name.is_empty() {
///             return AuthorizeResponse { allow: false, reason: Some("name required".into()) };
///         }
///         AuthorizeResponse { allow: true, reason: None }
///     }
/// }
///
/// register_plugin!(MyPlugin);
/// ```
pub trait Plugin: Default + 'static {
    fn authorize(&self, task: TaskInfo) -> AuthorizeResponse {
        let _ = task;
        AuthorizeResponse {
            allow: true,
            reason: None,
        }
    }

    fn enrich(&self, task: TaskInfo) -> EnrichResponse {
        let _ = task;
        EnrichResponse { env: Vec::new() }
    }

    fn on_task_start(&self, task: TaskInfo) {
        let _ = task;
    }

    fn on_task_complete(&self, result: TaskResult) {
        let _ = result;
    }
}

/// Register a type as a Propeller proplet plugin.
///
/// The type must implement [`Plugin`] and [`Default`]. The macro generates all
/// required WIT component exports so you don't have to write any WASM ABI code.
///
/// ```ignore
/// register_plugin!(MyPlugin);
/// ```
#[macro_export]
macro_rules! register_plugin {
    ($plugin_type:ty) => {
        struct __PropletPluginImpl;

        impl $crate::exports::propeller::proplet_plugin::lifecycle::Guest
            for __PropletPluginImpl
        {
            fn authorize(task: $crate::TaskInfo) -> $crate::AuthorizeResponse {
                <$plugin_type as $crate::Plugin>::authorize(
                    &<$plugin_type as ::std::default::Default>::default(),
                    task,
                )
            }

            fn enrich(task: $crate::TaskInfo) -> $crate::EnrichResponse {
                <$plugin_type as $crate::Plugin>::enrich(
                    &<$plugin_type as ::std::default::Default>::default(),
                    task,
                )
            }

            fn on_task_start(task: $crate::TaskInfo) {
                <$plugin_type as $crate::Plugin>::on_task_start(
                    &<$plugin_type as ::std::default::Default>::default(),
                    task,
                );
            }

            fn on_task_complete(result: $crate::TaskResult) {
                <$plugin_type as $crate::Plugin>::on_task_complete(
                    &<$plugin_type as ::std::default::Default>::default(),
                    result,
                );
            }
        }

        $crate::export!(__PropletPluginImpl with_types_in $crate);
    };
}
