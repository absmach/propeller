CGO_ENABLED ?= 0
GOOS ?= linux
GOARCH ?= amd64
BUILD_DIR = build
TIME=$(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
VERSION ?= $(shell git describe --abbrev=0 --tags 2>/dev/null || echo 'v0.0.0')
COMMIT ?= $(shell git rev-parse HEAD)
EXAMPLES = addition long-addition
SERVICES = manager proplet cli

define compile_service
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
	go build -ldflags "-s -w \
	-X 'github.com/absmach/magistrala.BuildTime=$(TIME)' \
	-X 'github.com/absmach/magistrala.Version=$(VERSION)' \
	-X 'github.com/absmach/magistrala.Commit=$(COMMIT)'" \
	-o ${BUILD_DIR}/$(1) cmd/$(1)/main.go
endef

$(SERVICES):
	$(call compile_service,$(@))

install:
	for file in $(BUILD_DIR)/*; do \
		if [[ ! "$$file" =~ \.wasm$$ ]]; then \
			cp "$$file" $(GOBIN)/propeller-`basename "$$file"`; \
		fi \
	done

.PHONY: all $(SERVICES)
all: $(SERVICES)

clean:
	rm -rf build

lint:
	golangci-lint run  --config .golangci.yaml

start-magistrala:
	docker compose -f docker/compose.yaml up -d

stop-magistrala:
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
