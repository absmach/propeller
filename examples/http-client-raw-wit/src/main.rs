/// Raw WIT bindgen HTTP client example for proplet.
///
/// Uses `wasi:http/outgoing-handler` directly (no `wstd`) to make both
/// plain HTTP and HTTPS GET requests. This is useful for diagnosing the
/// scheme-upgrade bug where HTTP requests are silently promoted to HTTPS.
///
/// Build:
///   cargo build --release
///   (produces target/wasm32-wasip2/release/http_client_raw.wasm)
///
/// Run locally with wasmtime:
///   wasmtime -Shttp target/wasm32-wasip2/release/http_client_raw.wasm
///
/// Run via proplet:
///   Set PROPLET_HTTP_ENABLED=true on the proplet.
///   Upload as a WASI P2 command task (daemon=false).

#[allow(warnings)]
mod bindings {
    wit_bindgen::generate!({
        world: "http-client-raw-wit",
        path: "wit",
        generate_all,
    });
}

use bindings::wasi::http::outgoing_handler;
use bindings::wasi::http::types::{
    ErrorCode, Fields, OutgoingBody, OutgoingRequest, Scheme,
};

fn main() {
    eprintln!("=== Test 1: plain HTTP GET (http://httpbin.org/get) ===");
    match fetch("httpbin.org", "/get", false) {
        Ok((status, body)) => {
            eprintln!("HTTP  status={status}");
            println!("{}", String::from_utf8_lossy(&body));
        }
        Err(e) => {
            eprintln!("HTTP  FAILED: {e}");
            eprintln!("  -> scheme may have been upgraded to HTTPS (port 443)");
        }
    }

    eprintln!();
    eprintln!("=== Test 2: HTTPS GET (https://httpbin.org/get) ===");
    match fetch("httpbin.org", "/get", true) {
        Ok((status, body)) => {
            eprintln!("HTTPS status={status}");
            println!("{}", String::from_utf8_lossy(&body));
        }
        Err(e) => {
            eprintln!("HTTPS FAILED: {e}");
        }
    }
}

bindings::export!(Main with_types_in bindings);

struct Main;

impl bindings::exports::wasi::cli::run::Guest for Main {
    fn run() -> Result<(), ()> {
        main();
        Ok(())
    }
}

fn fetch(
    authority: &str,
    path: &str,
    use_tls: bool,
) -> Result<(u16, Vec<u8>), String> {
    let headers = Fields::new();
    let request = OutgoingRequest::new(headers);
    request
        .set_scheme(Some(if use_tls {
            &Scheme::Https
        } else {
            &Scheme::Http
        }))
        .map_err(|()| "set_scheme failed")?;
    request
        .set_authority(Some(authority))
        .map_err(|()| "set_authority failed")?;
    request
        .set_path_with_query(Some(path))
        .map_err(|()| "set_path_with_query failed")?;

    let outgoing_body = request
        .body()
        .map_err(|()| "failed to get request body")?;
    OutgoingBody::finish(outgoing_body, None).map_err(|e| format!("failed to finish body: {e:?}"))?;

    let response = outgoing_handler::handle(request, None)
        .map_err(|e| format!("outgoing-handler error: {e:?}"))?;

    let incoming = block_on_future_incoming_response(response)
        .map_err(|e| format!("request error: {e:?}"))?;

    let status = incoming.status();
    let body = consume_body(
        incoming
            .consume()
            .map_err(|()| "body already consumed")?,
    );

    Ok((status, body))
}

fn block_on_future_incoming_response(
    future: bindings::wasi::http::types::FutureIncomingResponse,
) -> Result<bindings::wasi::http::types::IncomingResponse, ErrorCode> {
    let pollable = future.subscribe();
    loop {
        match future.get() {
            Some(Ok(Ok(response))) => return Ok(response),
            Some(Ok(Err(error_code))) => return Err(error_code),
            Some(Err(())) => return Err(ErrorCode::HttpProtocolError),
            None => pollable.block(),
        }
    }
}

fn consume_body(body: bindings::wasi::http::types::IncomingBody) -> Vec<u8> {
    let stream = match body.stream() {
        Ok(s) => s,
        Err(()) => return Vec::new(),
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
        if stream.read(0).is_err() {
            break;
        }
    }
    data
}
