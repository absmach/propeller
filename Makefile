CGO_ENABLED ?= 0
GOOS ?= linux
GOARCH ?= amd64
BUILD_DIR = build
TIME=$(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
VERSION ?= $(shell git describe --abbrev=0 --tags 2>/dev/null || echo 'v0.0.0')
COMMIT ?= $(shell git rev-parse HEAD)
EXAMPLES = addition compute hello-world
SERVICES = manager proplet cli proxy

define compile_service
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
	go build -ldflags "-s -w \
	-X 'github.com/absmach/supermq.BuildTime=$(TIME)' \
	-X 'github.com/absmach/supermq.Version=$(VERSION)' \
	-X 'github.com/absmach/supermq.Commit=$(COMMIT)'" \
	-o ${BUILD_DIR}/$(1) cmd/$(1)/main.go
endef

$(SERVICES):
	$(call compile_service,$(@))


# Install all non-WASM executables from the build directory to GOBIN with 'propeller-' prefix
install:
	$(foreach f,$(wildcard $(BUILD_DIR)/*[!.wasm]),cp $(f) $(patsubst $(BUILD_DIR)/%,$(GOBIN)/propeller-%,$(f));)

.PHONY: all $(SERVICES) $(EXAMPLES)
all: $(SERVICES) $(EXAMPLES)

clean:
	rm -rf build

lint:
	golangci-lint run  --config .golangci.yaml

start-supermq:
	docker compose -f docker/compose.yaml up -d

stop-supermq:
	docker compose -f docker/compose.yaml down

$(EXAMPLES):
	GOOS=js GOARCH=wasm tinygo build -o build/$@.wasm -target wasi examples/$@/$@.go

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Targets:"
	@echo "  <service>:        build the binary for the service i.e manager, proplet, cli"
	@echo "  all:              build all binaries i.e manager, proplet, cli"
	@echo "  install:          install the binary i.e copies to GOBIN"
	@echo "  clean:            clean the build directory"
	@echo "  lint:             run golangci-lint"
	@echo "  start-magistrala: start the magistrala docker compose"
	@echo "  stop-magistrala:  stop the magistrala docker compose"
	@echo "  help:             display this help message"
