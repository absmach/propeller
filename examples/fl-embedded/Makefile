# Makefile for building FL embedded example

.PHONY: build clean

# Build WASM module using TinyGo
build: fl-client.wasm

fl-client.wasm: fl-client.go
	@echo "Building WASM module with TinyGo..."
	tinygo build -target=wasi -o fl-client.wasm fl-client.go
	@echo "Build complete: fl-client.wasm"

# Clean build artifacts
clean:
	rm -f fl-client.wasm
	@echo "Cleaned build artifacts"

# Help target
help:
	@echo "Available targets:"
	@echo "  build  - Build the WASM module (default)"
	@echo "  clean  - Remove build artifacts"
	@echo "  help   - Show this help message"
