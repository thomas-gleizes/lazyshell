BINARY  := bin/lazyshell
PKG     := ./cmd/lazyshell
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/thomas-gleizes/lazyshell/pkg/version.Version=$(VERSION)

.PHONY: all build run test lint fmt vet clean demo generate

all: fmt vet lint test build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

# Regenerates site/assets/config-schema.json from pkg/config's structs and
# README.md/docs/README.fr.md's reference tables (cmd/gen-config-schema).
# Not part of `all`: nothing else in this Makefile touches site/, and CI
# checks the committed file is up to date (git diff --exit-code) instead of
# regenerating it on every build.
generate:
	go generate ./...

# Regenerates docs/demo.gif from docs/demo.tape. Requires vhs
# (https://github.com/charmbracelet/vhs) installed locally; not run in CI.
# The tape types a bare "lazyshell" — depends on build and prepends bin/ to
# PATH so the recording exercises what was just compiled, not whatever
# (possibly stale, possibly absent) lazyshell happens to already be on the
# machine's PATH.
demo: build
	PATH="$(CURDIR)/bin:$$PATH" vhs docs/demo.tape

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
