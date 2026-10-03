.PHONY: all build build-cli build-server test test-race bench ui ui-build clean help

BIN_DIR := bin
CLI_BIN := $(BIN_DIR)/codepilot
SERVER_BIN := $(BIN_DIR)/codepilot-server

all: build test

build: build-cli build-server

build-cli:
	@mkdir -p $(BIN_DIR)
	go build -o $(CLI_BIN) ./cmd/codepilot

build-server:
	@mkdir -p $(BIN_DIR)
	go build -o $(SERVER_BIN) ./cmd/codepilot-server

test:
	go test -v ./internal/... ./test/benchmarks/...

test-race:
	go test -race -v ./internal/... ./test/benchmarks/...

bench:
	go test -v -run 'Benchmark|Scale' ./test/benchmarks/...

ui:
	cd web && npm run dev

ui-build:
	cd web && npm install && npm run build

clean:
	rm -rf $(BIN_DIR) codepilot codepilot-server web/dist
