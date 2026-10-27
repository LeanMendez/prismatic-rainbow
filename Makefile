BINARY  := prismatic-rainbow
# Short name installed as a symlink next to the binary (`pr` is taken by coreutils).
ALIAS   := prb
GOBIN   := $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test lint install clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/$(BINARY)

test:
	go test ./...

lint:
	go vet ./...
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt:"; echo "$$out"; exit 1; fi

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)
	ln -sf $(BINARY) $(GOBIN)/$(ALIAS)

clean:
	rm -rf bin dist
