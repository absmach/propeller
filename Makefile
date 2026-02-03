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
# Note: proplet images are built separately via docker_proplet and docker_proplet_wasinn targets
DOCKER_IMAGE_NAME_PREFIX ?= ghcr.io/absmach/propeller
PROPLET_IMAGE = $(DOCKER_IMAGE_NAME_PREFIX)/proplet

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

define docker_push
		for svc in $(SERVICES); do \
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

dockers: $(DOCKERS)
dockers_dev: $(DOCKERS_DEV)

# Build proplet base image (lightweight, no wasi-nn)
docker_proplet:
	docker build \
		--no-cache \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg TIME=$(TIME) \
		--tag=$(PROPLET_IMAGE):latest \
		-f docker/Dockerfile.proplet .

# Build proplet wasi-nn image (with OpenVINO support, x86_64 only)
docker_proplet_wasinn:
	docker build \
		--no-cache \
		--platform linux/amd64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg TIME=$(TIME) \
		--tag=$(PROPLET_IMAGE):wasi-nn \
		-f docker/Dockerfile.proplet-wasinn .

# Build both proplet images
docker_proplet_all: docker_proplet docker_proplet_wasinn


push_proplet:
	docker push $(PROPLET_IMAGE):latest

push_proplet_wasinn:
	docker push $(PROPLET_IMAGE):wasi-nn

push_proplet_all: push_proplet push_proplet_wasinn

latest: dockers docker_proplet
		$(call docker_push,latest)
		$(MAKE) push_proplet

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
	@echo "  <service>:            build the binary for the service i.e manager, proplet, cli"
	@echo "  all:                  build all binaries (Go: manager, cli; Rust: proplet)"
	@echo "  proplet:              build the Rust proplet binary"
	@echo "  install:              install the binary i.e copies to GOBIN"
	@echo "  clean:                clean the build directory and Rust target"
	@echo "  lint:                 run golangci-lint"
	@echo "  test:             run FL unit and integration tests"
	@echo "  test-all:         run all tests (Go and Rust)"
	@echo ""
	@echo "Docker targets:"
	@echo "  dockers:              build all Go service Docker images"
	@echo "  dockers_dev:          build all Go service dev Docker images"
	@echo "  docker_proplet:       build proplet base image (lightweight, no wasi-nn)"
	@echo "  docker_proplet_wasinn: build proplet wasi-nn image (with OpenVINO, x86_64 only)"
	@echo "  docker_proplet_all:   build both proplet images"
	@echo "  push_proplet:         push proplet:latest image"
	@echo "  push_proplet_wasinn:  push proplet:wasi-nn image"
	@echo "  push_proplet_all:     push both proplet images"
	@echo "  latest:               build and push all Docker images"
	@echo ""
	@echo "SuperMQ targets:"
	@echo "  start-supermq:        start the supermq docker compose"
	@echo "  stop-supermq:         stop the supermq docker compose"
	@echo ""
	@echo "  help:                 display this help message"
