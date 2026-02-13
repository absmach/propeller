/// HTTP server example for proplet.
///
/// Implements `wasi:http/proxy` world via raw `wit-bindgen` — no `wstd`,
/// so it works with both `wasmtime serve` and proplet's embedded runtime.
///
/// Routes:
///   GET  /          → 200 "Hello from proplet WASM HTTP server!\n"
///   GET  /health    → 200 "ok\n"
///   POST /echo      → 200 <mirrors request body back>
///   GET  /headers   → 200 plain-text list of request headers
///   *               → 404 "Not Found\n"

#[allow(warnings)]
mod bindings {
    wit_bindgen::generate!({
        world: "http-server",
        path: "wit",
        generate_all,
    });
}

use bindings::exports::wasi::http::incoming_handler::Guest;
use bindings::wasi::http::types::{
    Fields, IncomingRequest, OutgoingBody, OutgoingResponse, ResponseOutparam,
};

struct HttpServer;

bindings::export!(HttpServer with_types_in bindings);

impl Guest for HttpServer {
    fn handle(request: IncomingRequest, response_out: ResponseOutparam) {
        let method = request.method();
        let path_with_query = request.path_with_query().unwrap_or_else(|| "/".to_string());
        let path = path_with_query.split('?').next().unwrap_or("/").to_string();

        use bindings::wasi::http::types::Method;
        let method_str = match method {
            Method::Get => "GET",
            Method::Post => "POST",
            Method::Put => "PUT",
            Method::Delete => "DELETE",
            Method::Head => "HEAD",
            Method::Options => "OPTIONS",
            Method::Patch => "PATCH",
            _ => "OTHER",
        };

        match (method_str, path.as_str()) {
            ("GET", "/") => {
                send_response(response_out, 200, b"Hello from proplet WASM HTTP server!\n")
            }
            ("GET", "/health") => send_response(response_out, 200, b"ok\n"),
            ("POST", "/echo") => echo_response(request, response_out),
            ("GET", "/headers") => headers_response(request, response_out),
            _ => send_response(response_out, 404, b"Not Found\n"),
        }
    }
}

fn send_response(response_out: ResponseOutparam, status: u16, body: &[u8]) {
    let headers = Fields::new();
    let response = OutgoingResponse::new(headers);
    response.set_status_code(status).unwrap();

    let outgoing_body = response.body().unwrap();
    {
        let stream = outgoing_body.write().unwrap();
        stream.write(body).unwrap();
        drop(stream);
    }
    OutgoingBody::finish(outgoing_body, None).unwrap();
    ResponseOutparam::set(response_out, Ok(response));
}

fn echo_response(request: IncomingRequest, response_out: ResponseOutparam) {
    let body_bytes = read_body(request);

    let headers = Fields::new();
    let response = OutgoingResponse::new(headers);
    response.set_status_code(200).unwrap();

    let outgoing_body = response.body().unwrap();
    {
        let stream = outgoing_body.write().unwrap();
        stream.write(&body_bytes).unwrap();
        drop(stream);
    }
    OutgoingBody::finish(outgoing_body, None).unwrap();
    ResponseOutparam::set(response_out, Ok(response));
}

fn headers_response(request: IncomingRequest, response_out: ResponseOutparam) {
    let req_headers = request.headers();
    let mut pairs: Vec<(String, String)> = req_headers
        .entries()
        .into_iter()
        .map(|(name, value)| {
            let v = String::from_utf8_lossy(&value).to_string();
            (name, v)
        })
        .collect();
    pairs.sort_by(|a, b| a.0.cmp(&b.0));

    let mut lines = String::new();
    for (name, value) in &pairs {
        lines.push_str(&format!("{name}: {value}\n"));
    }

    let out_headers = Fields::new();
    out_headers
        .set(&"content-type".to_string(), &[b"text/plain".to_vec()])
        .unwrap();
    let response = OutgoingResponse::new(out_headers);
    response.set_status_code(200).unwrap();

    let outgoing_body = response.body().unwrap();
    {
        let stream = outgoing_body.write().unwrap();
        stream.write(lines.as_bytes()).unwrap();
        drop(stream);
    }
    OutgoingBody::finish(outgoing_body, None).unwrap();
    ResponseOutparam::set(response_out, Ok(response));
}

/// Drain the incoming request body into a Vec<u8>.
fn read_body(request: IncomingRequest) -> Vec<u8> {
    let body = match request.consume() {
        Ok(b) => b,
        Err(_) => return Vec::new(),
    };
    let stream = match body.stream() {
        Ok(s) => s,
        Err(_) => return Vec::new(),
    };

    let mut data = Vec::new();
    loop {
        match stream.read(65536) {
            Ok(chunk) if chunk.is_empty() => {
                let pollable = stream.subscribe();
                pollable.block();
            }
            Ok(chunk) => data.extend_from_slice(&chunk),
            Err(_) => break,
        }
        match stream.read(0) {
            Err(_) => break,
            Ok(_) => {}
        }
    }
    data
}
