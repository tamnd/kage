BIN     := kage
PKG     := ./cmd/kage
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/tamnd/kage/cli.Version=$(VERSION) \
	-X github.com/tamnd/kage/cli.Commit=$(COMMIT) \
	-X github.com/tamnd/kage/cli.Date=$(DATE)

export CGO_ENABLED := 0

.PHONY: build build-webview install test test-short vet tidy clean run

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BIN) $(PKG)

# A native-window viewer: opens packed sites in their own OS WebView window
# instead of the browser. Needs cgo, so it is built separately from the default
# pure-Go binary and the release pipeline.
build-webview:
	CGO_ENABLED=1 go build -tags webview -ldflags "$(LDFLAGS)" -o bin/$(BIN) $(PKG)

install:
	go install -ldflags "$(LDFLAGS)" $(PKG)

# Full suite, including the Chrome-driven end-to-end tests.
test:
	go test -race ./...

# Skip the tests that launch a real browser (CI without Chrome, quick loops).
test-short:
	go test -short ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin

run: build
	./bin/$(BIN)
