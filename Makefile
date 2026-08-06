BINARY  := bin/lazyshell
PKG     := ./cmd/lazyshell
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/thomas-gleizes/lazyshell/pkg/version.Version=$(VERSION)

.PHONY: all build run test lint fmt vet clean demo

all: fmt vet lint test build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

# Regenerates docs/demo.gif from docs/demo.tape. Requires vhs
# (https://github.com/charmbracelet/vhs) installed locally; not run in CI.
demo:
	vhs docs/demo.tape

run:
	go run $(PKG)

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -l -w .

vet:
	go vet ./...

clean:
	rm -rf bin coverage.out
