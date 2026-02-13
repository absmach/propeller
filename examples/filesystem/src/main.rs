/// Filesystem example for proplet.
///
/// This WASI P1 module writes a file to a preopened directory, reads it back,
/// and prints the result to stdout — verifying that the guest has read/write
/// access to the host filesystem.
///
/// Build:
///   cargo build --release
///   (produces target/wasm32-wasip1/release/filesystem.wasm)
///
/// Run locally with wasmtime (preopens /tmp):
///   wasmtime --dir /tmp target/wasm32-wasip1/release/filesystem.wasm
///
/// Run via proplet:
///   Set PROPLET_DIRS=/tmp on the proplet.
///   Upload the .wasm as a WASI P1 task (no --invoke needed,
///   _start / main is called automatically).
use std::fs;
use std::io::{Read, Write};
use std::path::Path;
use std::time::{SystemTime, UNIX_EPOCH};

fn main() {
    let dir = Path::new("/tmp");

    // Use a timestamped filename so repeated runs don't collide.
    let ts = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs();
    let path = dir.join(format!("proplet-fs-test-{ts}.txt"));

    // --- Write ---
    let content = format!(
        "Proplet filesystem test\nTimestamp: {ts}\nPath: {}\n",
        path.display()
    );
    println!("Writing {} bytes to {}", content.len(), path.display());
    {
        let mut file = fs::File::create(&path)
            .unwrap_or_else(|e| panic!("Failed to create {}: {e}", path.display()));
        file.write_all(content.as_bytes())
            .unwrap_or_else(|e| panic!("Failed to write {}: {e}", path.display()));
    }
    println!("Write OK");

    // --- Read back ---
    let mut read_back = String::new();
    {
        let mut file = fs::File::open(&path)
            .unwrap_or_else(|e| panic!("Failed to open {}: {e}", path.display()));
        file.read_to_string(&mut read_back)
            .unwrap_or_else(|e| panic!("Failed to read {}: {e}", path.display()));
    }
    println!("Read back {} bytes:", read_back.len());
    print!("{read_back}");

    // --- Verify round-trip ---
    assert_eq!(
        content, read_back,
        "Read-back content does not match written content"
    );
    println!("Round-trip verification OK");

    // --- Append ---
    {
        let mut file = fs::OpenOptions::new()
            .append(true)
            .open(&path)
            .unwrap_or_else(|e| panic!("Failed to open for append {}: {e}", path.display()));
        file.write_all(b"Appended line\n")
            .unwrap_or_else(|e| panic!("Failed to append {}: {e}", path.display()));
    }
    println!("Append OK");

    // --- List directory ---
    println!("Listing {}:", dir.display());
    let mut entries: Vec<_> = fs::read_dir(dir)
        .unwrap_or_else(|e| panic!("Failed to read dir {}: {e}", dir.display()))
        .filter_map(|e| e.ok())
        .map(|e| e.file_name().to_string_lossy().into_owned())
        .collect();
    entries.sort();
    for entry in &entries {
        println!("  {entry}");
    }

    // --- Delete ---
    fs::remove_file(&path).unwrap_or_else(|e| panic!("Failed to delete {}: {e}", path.display()));
    println!("Delete OK");

    println!("All filesystem operations succeeded");
}
