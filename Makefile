BINARY := bin/lazyshell
PKG    := ./cmd/lazyshell

.PHONY: all build run test lint fmt vet clean

all: fmt vet lint test build

build:
	go build -o $(BINARY) $(PKG)

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
