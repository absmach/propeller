wit_bindgen::generate!({ world: "greet-world" });

struct Component;

impl Guest for Component {
    fn greet(name: String) -> String {
        format!("Hello, {}!", name)
    }
}

export!(Component);
