wit_bindgen::generate!({
    world: "http-greet",
    generate_all,
});

use wasi::http::outgoing_handler::OutgoingRequest;
use wasi::http::types::{
    Fields, Method, Scheme,
};

struct Component;

impl Guest for Component {
    fn my_function() -> String {
        let headers = Fields::new();
        let request = OutgoingRequest::new(headers);
        request.set_method(&Method::Get).ok();
        request.set_path_with_query(Some("/")).ok();
        request.set_scheme(Some(&Scheme::Https)).ok();
        request.set_authority(Some("example.com")).ok();
        "custom-export with wasi:http ready".to_string()
    }
}

export!(Component);
