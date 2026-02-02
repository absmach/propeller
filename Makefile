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

define compile_service
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
	go build -ldflags "-s -w \
	-X 'github.com/absmach/supermq.BuildTime=$(TIME)' \
	-X 'github.com/absmach/supermq.Version=$(VERSION)' \
	-X 'github.com/absmach/supermq.Commit=$(COMMIT)'" \
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
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc) \
		-f docker/Dockerfile .
endef

define make_docker_dev
	$(eval svc=$(subst docker_dev_,,$(1)))

	docker build \
		--no-cache \
		--build-arg SVC=$(svc) \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc) \
		-f docker/Dockerfile.dev ./build
endef

define make_docker_rust
	$(eval svc=$(subst docker_,,$(1)))

	docker build \
		--no-cache \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg TIME=$(TIME) \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc) \
		-f docker/Dockerfile.$(svc) .
endef

define make_docker_rust_dev
	$(eval svc=$(subst docker_dev_,,$(1)))

	docker build \
		--no-cache \
		--build-arg SVC=$(svc) \
		--tag=$(DOCKER_IMAGE_NAME_PREFIX)/$(svc) \
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

$(SERVICES):
	$(call compile_service,$(@))

$(RUST_SERVICES):
	cd proplet && cargo build --release

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

# Install all non-WASM executables from the build directory to GOBIN with 'propeller-' prefix
install:
	$(foreach f,$(wildcard $(BUILD_DIR)/*[!.wasm]),cp $(f) $(patsubst $(BUILD_DIR)/%,$(GOBIN)/propeller-%,$(f));)

.PHONY: all $(SERVICES) $(RUST_SERVICES) $(EXAMPLES)
all: $(SERVICES) $(RUST_SERVICES) $(EXAMPLES)

clean:
	rm -rf build
	cd proplet && cargo clean

lint:
	golangci-lint run  --config .golangci.yaml
	cd proplet && cargo check --release && cargo fmt --all -- --check && cargo clippy -- -D warnings

test:
	go test -v ./manager

test-all:
	go test -v ./...
	cd proplet && cargo test --release

start-supermq:
	docker compose -f docker/compose.yaml --env-file docker/.env up -d

stop-supermq:
	docker compose -f docker/compose.yaml --env-file docker/.env down

$(EXAMPLES):
	GOOS=js GOARCH=wasm tinygo build -buildmode=c-shared -o build/$@.wasm -target wasip1 examples/$@/$@.go

addition-wat:
	@wat2wasm examples/addition-wat/addition.wat -o build/addition-wat.wasm
	@base64 build/addition-wat.wasm > build/addition-wat.b64

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Targets:"
	@echo "  <service>:        build the binary for the service i.e manager, proplet, cli"
	@echo "  all:              build all binaries (Go: manager, cli; Rust: proplet)"
	@echo "  proplet:          build the Rust proplet binary"
	@echo "  install:          install the binary i.e copies to GOBIN"
	@echo "  clean:            clean the build directory and Rust target"
	@echo "  lint:             run golangci-lint"
	@echo "  test:             run FL unit and integration tests"
	@echo "  test-all:         run all tests (Go and Rust)"
	@echo "  dockers:          build and push all Docker images (Go and Rust services)"
	@echo "  dockers_dev:      build all Go service dev Docker images"
	@echo "  dockers_rust:     build all Rust service Docker images"
	@echo "  dockers_rust_dev: build all Rust service dev Docker images"
	@echo "  latest:           build and push all Go service Docker images"
	@echo "  start-supermq:    start the supermq docker compose"
	@echo "  stop-supermq:     stop the supermq docker compose"
	@echo "  help:             display this help message"
