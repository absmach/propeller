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
use wstd::http::{Client, IntoBody, Method, Request};
use wstd::io::AsyncRead;

#[wstd::main]
async fn main() {
    let client = Client::new();

    // --- 1. GET against an external endpoint ---
    println!("=== GET https://httpbin.org/get ===");
    let req = Request::get("https://httpbin.org/get")
        .body(b"".into_body())
        .unwrap();
    let mut res = client.send(req).await.expect("GET httpbin failed");
    println!("Status: {}", res.status());
    let mut body = Vec::new();
    res.body_mut()
        .read_to_end(&mut body)
        .await
        .expect("failed to read body");
    println!("{}\n", String::from_utf8_lossy(&body));

    // --- 2. POST with a body against an external echo endpoint ---
    println!("=== POST https://httpbin.org/post ===");
    let payload = b"hello from proplet wasm client";
    let req = Request::builder()
        .method(Method::POST)
        .uri("https://httpbin.org/post")
        .header("content-type", "text/plain")
        .body(payload.into_body())
        .unwrap();
    let mut res = client.send(req).await.expect("POST httpbin failed");
    println!("Status: {}", res.status());
    let mut body = Vec::new();
    res.body_mut()
        .read_to_end(&mut body)
        .await
        .expect("failed to read body");
    println!("{}\n", String::from_utf8_lossy(&body));
}
