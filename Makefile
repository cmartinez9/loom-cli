.PHONY: build test lint install clean

BIN_EXT := $(if $(filter windows,$(shell go env GOOS)),.exe,)

build:
	go build -trimpath -o bin/loom-pp-cli$(BIN_EXT) ./cmd/loom-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/loom-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -trimpath -o bin/loom-pp-mcp$(BIN_EXT) ./cmd/loom-pp-mcp

install-mcp:
	go install ./cmd/loom-pp-mcp

build-all: build build-mcp
