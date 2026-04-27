CGO_ENABLED ?= 0
GOOS ?= linux
GOARCH ?= amd64
BUILD_DIR = build
TIME=$(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
VERSION ?= $(shell git describe --abbrev=0 --tags 2>/dev/null || echo 'v0.0.0')
COMMIT ?= $(shell git rev-parse HEAD)
EXAMPLES = addition compute hello-world
SERVICES = manager cli proxy
RUST_SERVICES = proplet
DOCKERS = $(addprefix docker_,$(SERVICES))
DOCKERS_DEV = $(addprefix docker_dev_,$(SERVICES))
DOCKERS_RUST = $(addprefix docker_,$(RUST_SERVICES))
DOCKERS_RUST_DEV = $(addprefix docker_dev_,$(RUST_SERVICES))
DOCKER_IMAGE_NAME_PREFIX ?= ghcr.io/absmach/propeller
WASMTIME_VERSION ?= 42.0.1

define compile_service
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
	go build -ldflags "-s -w \
	-X 'github.com/absmach/magistrala.BuildTime=$(TIME)' \
	-X 'github.com/absmach/magistrala.Version=$(VERSION)' \
	-X 'github.com/absmach/magistrala.Commit=$(COMMIT)'" \
	-o ${BUILD_DIR}/$(1) cmd/$(1)/main.go
endef

define make_docker
	$(eval svc=$(subst docker_,,$(1)))

	docker build \
		--no-cache \
		--build-arg SVC=$(svc) \
		--build-arg GOARCH=$(GOARCH) \
		--build-arg GOARM=$(GOARM) \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg TIME=$(TIME) \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc):latest \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc):$(COMMIT) \
		-f docker/Dockerfile .
endef

define make_docker_dev
	$(eval svc=$(subst docker_dev_,,$(1)))

	docker build \
		--no-cache \
		--build-arg SVC=$(svc) \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc):latest \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc):$(COMMIT) \
		-f docker/Dockerfile.dev ./build
endef

define make_docker_rust
	$(eval svc=$(subst docker_,,$(1)))

	docker build \
		--no-cache \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg TIME=$(TIME) \
		--build-arg WASMTIME_VERSION=$(WASMTIME_VERSION) \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc):latest \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc):$(COMMIT) \
		-f docker/Dockerfile.$(svc) .
endef

define make_docker_rust_dev
	$(eval svc=$(subst docker_dev_,,$(1)))

	docker build \
		--no-cache \
		--build-arg SVC=$(svc) \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc):latest \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc):$(COMMIT) \
		-f docker/Dockerfile.$(svc).dev ./proplet/target/release
endef

define docker_push
		for svc in $(SERVICES); do \
			docker push $(DOCKER_IMAGE_NAME_PREFIX)/$$svc:$(1); \
		done
		for svc in $(RUST_SERVICES); do \
			docker push $(DOCKER_IMAGE_NAME_PREFIX)/$$svc:$(1); \
		done
endef

.DEFAULT_GOAL := all

$(SERVICES):
	$(call compile_service,$(@))

$(RUST_SERVICES):
	cd proplet && cargo build --release && cp target/release/proplet ../build

$(DOCKERS):
	$(call make_docker,$(@),$(GOARCH))

$(DOCKERS_DEV):
	$(call make_docker_dev,$(@))

$(DOCKERS_RUST):
	$(call make_docker_rust,$(@))

$(DOCKERS_RUST_DEV):
	$(call make_docker_rust_dev,$(@))

dockers: $(DOCKERS)
dockers_dev: $(DOCKERS_DEV)
dockers_rust: $(DOCKERS_RUST)
dockers_rust_dev: $(DOCKERS_RUST_DEV)

latest: dockers dockers_rust
		$(call docker_push,latest)

docker_proplet_wasinn:
	docker build \
		--no-cache \
		--platform linux/amd64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg TIME=$(TIME) \
		--build-arg WASMTIME_VERSION=$(WASMTIME_VERSION) \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/proplet:wasi-nn \
		-f docker/Dockerfile.proplet-wasinn .

push_proplet_wasinn:
	docker push $(DOCKER_IMAGE_NAME_PREFIX)/proplet:wasi-nn

# Install all non-WASM executables from the build directory to GOBIN with 'propeller-' prefix
install:
	$(foreach f,$(wildcard $(BUILD_DIR)/*[!.wasm]),cp $(f) $(patsubst $(BUILD_DIR)/%,$(GOBIN)/propeller-%,$(f));)

.PHONY: all $(SERVICES) $(RUST_SERVICES) $(EXAMPLES) docker_proplet_wasinn push_proplet_wasinn mocks start-magistrala stop-magistrala start-propeller stop-propeller start-all stop-all
all: $(SERVICES) $(RUST_SERVICES) $(EXAMPLES) addition-wat http-client http-server filesystem

clean:
	rm -rf build
	cd proplet && cargo clean

mocks:
	@echo "Generating mocks..."
	@which mockery > /dev/null || (echo "mockery not found. Please install it from https://vektra.github.io/mockery/latest/installation/" && exit 1)
	mockery --config .mockery.yaml

lint:
	golangci-lint run  --config .golangci.yaml
	cd proplet && cargo check --release && cargo fmt --all -- --check && cargo clippy -- -D warnings

test: mocks
	go test -v ./manager

test-all:
	go test -v ./...
	cd proplet && cargo test --release

start-magistrala:
	docker compose -f docker/compose.yaml --env-file docker/.env up -d

stop-magistrala:
	docker compose -f docker/compose.yaml --env-file docker/.env down

start-propeller:
	docker compose -f docker/compose.propeller.yaml --env-file docker/.env up -d

stop-propeller:
	docker compose -f docker/compose.propeller.yaml --env-file docker/.env down

start-all:
	docker compose -f docker/compose.yaml --env-file docker/.env up -d
	docker compose -f docker/compose.propeller.yaml --env-file docker/.env up -d

stop-all:
	docker compose -f docker/compose.propeller.yaml --env-file docker/.env down
	docker compose -f docker/compose.yaml --env-file docker/.env down

$(EXAMPLES):
	GOOS=js GOARCH=wasm tinygo build -buildmode=c-shared -o build/$@.wasm -target wasi examples/$@/$@.go

addition-wat:
	@wat2wasm examples/addition-wat/addition.wat -o build/addition-wat.wasm
	@base64 build/addition-wat.wasm > build/addition-wat.b64

http-client:
	mkdir -p $(BUILD_DIR)
	cd examples/http-client && cargo build --release
	cp examples/http-client/target/wasm32-wasip2/release/http-client.wasm $(BUILD_DIR)/http-client.wasm

http-server:
	mkdir -p $(BUILD_DIR)
	cd examples/http-server && cargo build --release
	cp examples/http-server/target/wasm32-wasip2/release/http_server.wasm $(BUILD_DIR)/http-server.wasm

filesystem:
	mkdir -p $(BUILD_DIR)
	cd examples/filesystem && cargo build --release
	cp examples/filesystem/target/wasm32-wasip2/release/filesystem.wasm $(BUILD_DIR)/filesystem.wasm

hal-test:
	cd examples/hal-test && cargo build --release
	cp examples/hal-test/target/wasm32-wasip1/release/hal-test.wasm build/hal-test.wasm

attestation-test:
	cd examples/attestation-test && cargo build --release
	cp examples/attestation-test/target/wasm32-wasip1/release/attestation-test.wasm build/attestation-test.wasm

plugin-auth:
	mkdir -p $(BUILD_DIR)/plugins
	cd examples/plugin-auth && cargo build --target wasm32-wasip1 --release
	cp examples/plugin-auth/target/wasm32-wasip1/release/plugin_auth.wasm $(BUILD_DIR)/plugins/plugin-auth.wasm

proplet-plugin-example:
	mkdir -p $(BUILD_DIR)/proplet-plugins
	cd examples/proplet-plugin-example && cargo build --target wasm32-wasip2 --release
	cp examples/proplet-plugin-example/target/wasm32-wasip2/release/proplet_plugin_example.wasm $(BUILD_DIR)/proplet-plugins/proplet-plugin-example.wasm

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Targets:"
	@echo "  <service>:             build the binary for the service i.e manager, proplet, cli"
	@echo "  all:                   build all binaries (Go: manager, cli; Rust: proplet)"
	@echo "  proplet:               build the Rust proplet binary"
	@echo "  http-client:           build the WASI P2 HTTP client example (requires PROPLET_HTTP_ENABLED=true)"
	@echo "  http-server:           build the WASI P2 HTTP server example (requires PROPLET_HTTP_ENABLED=true, daemon=true)"
	@echo "  filesystem:            build the WASI P1 filesystem example (requires PROPLET_DIRS=/tmp)"
	@echo "  install:               install the binary i.e copies to GOBIN"
	@echo "  clean:                 clean the build directory and Rust target"
	@echo "  lint:                  run golangci-lint"
	@echo "  test:                  run FL unit and integration tests"
	@echo "  test-all:              run all tests (Go and Rust)"
	@echo "  dockers:               build and push all Docker images (Go and Rust services)"
	@echo "  dockers_dev:           build all Go service dev Docker images"
	@echo "  dockers_rust:          build all Rust service Docker images"
	@echo "  dockers_rust_dev:      build all Rust service dev Docker images"
	@echo "  docker_proplet_wasinn: build proplet wasi-nn Docker image"
	@echo "  push_proplet_wasinn:   push proplet:wasi-nn Docker image"
	@echo "  latest:                build and push all Go service Docker images"
	@echo "  start-magistrala:      start Magistrala services only"
	@echo "  stop-magistrala:       stop Magistrala services only"
	@echo "  start-propeller:       start Propeller services only (requires provisioned config.toml)"
	@echo "  stop-propeller:        stop Propeller services only"
	@echo "  start-all:             start Magistrala and Propeller services (requires provisioned config.toml)"
	@echo "  stop-all:              stop Magistrala and Propeller services"
	@echo "  mocks:                 generate mockery mocks for all interfaces"
	@echo "  help:                  display this help message"
