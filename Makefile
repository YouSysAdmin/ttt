VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := -trimpath
BIN_DIR := dist

# Binaries
BIN := $(BIN_DIR)/ttt

.PHONY: all help build run demo demo-db test test-v vet fmt lint clean

all: help

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk 'BEGIN {FS = ": "} {printf "  \033[36m%-10s\033[0m %s\n", $$1, substr($$0, length($$1) + 3)}'


## build: Build the ttt binary into dist/
build:
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/ttt

## run: Build and run (pass ARGS="..." for flags/subcommands)
run: build
	$(BIN) $(ARGS)

## demo: Build and run the TUI against the demo database
demo: build demo-db
	$(BIN) --db demo.db tui

## demo-db: Generate the demo database (demo.db)
demo-db:
	go run ./tools/demo-db demo.db

## test: Run all tests
test:
	go test ./...

## test-v: Run all tests with verbose output
test-v:
	go test -v ./...

## vet: Run go vet
vet:
	go vet ./...

## fmt: Run gofmt on all Go files
fmt:
	gofmt -w .

## lint: Run vet and check formatting
lint: vet
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:" && gofmt -l . && exit 1)

## clean: Remove built binaries and the demo database
clean:
	rm -rf $(BIN_DIR) demo.db
