/// HTTP client example for proplet.
///
/// This WASI P2 component makes outgoing HTTP requests using wasi:http/outgoing-handler.
/// It demonstrates GET, POST, and header inspection against the companion http-server example
/// as well as an external endpoint (httpbin.org).
///
/// Build:
///   cargo build --release
///   (produces target/wasm32-wasip2/release/http-client.wasm)
///
/// Run locally with wasmtime:
///   wasmtime -Shttp target/wasm32-wasip2/release/http-client.wasm
///
/// Run via proplet:
///   Set PROPLET_HTTP_ENABLED=true on the proplet.
///   Upload as a WASI P2 command task (daemon=false). The component entry
///   point is the `run` export, called automatically — no --invoke needed.
use wstd::http::{Body, Client, Method, Request};

#[wstd::main]
async fn main() {
    let client = Client::new();

    // --- 1. GET via plain HTTP (no TLS) — reproduces scheme-upgrade bug ---
    println!("=== GET http://httpbin.org/get ===");
    let req = Request::get("http://httpbin.org/get")
        .body(Body::empty())
        .unwrap();
    match client.send(req).await {
        Ok(mut res) => {
            println!("Status: {}", res.status());
            let body = res.body_mut().contents().await.expect("failed to read body");
            println!("{}\n", String::from_utf8_lossy(body));
        }
        Err(e) => {
            eprintln!("ERROR: plain HTTP GET failed: {e:?}");
            eprintln!("This likely means the scheme was upgraded to HTTPS (port 443) despite using http://");
        }
    }

    // --- 2. GET via HTTPS (baseline — should always work) ---
    println!("=== GET https://httpbin.org/get ===");
    let req = Request::get("https://httpbin.org/get")
        .body(Body::empty())
        .unwrap();
    let mut res = client.send(req).await.expect("GET httpbin HTTPS failed");
    println!("Status: {}", res.status());
    let body = res.body_mut().contents().await.expect("failed to read body");
    println!("{}\n", String::from_utf8_lossy(body));

    // --- 3. POST with a body against an external echo endpoint ---
    println!("=== POST https://httpbin.org/post ===");
    let payload = b"hello from proplet wasm client";
    let req = Request::builder()
        .method(Method::POST)
        .uri("https://httpbin.org/post")
        .header("content-type", "text/plain")
        .body(Body::from(payload.as_slice()))
        .unwrap();
    let mut res = client.send(req).await.expect("POST httpbin failed");
    println!("Status: {}", res.status());
    let body = res.body_mut().contents().await.expect("failed to read body");
    println!("{}\n", String::from_utf8_lossy(body));
}
