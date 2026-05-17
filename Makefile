.PHONY: build build-all test lint vet clean install release

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -X main.version=$(VERSION) -s -w
GO = go
GOFLAGS = -trimpath

build:
	@rm -rf bin/
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocal ./cmd/pastelocal
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocald ./cmd/pastelocald
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocal-remote ./cmd/pastelocal-remote

build-all: clean build-linux-amd64 build-linux-arm64 build-darwin-arm64 build-darwin-amd64

build-linux-amd64:
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocal-remote-linux-amd64 ./cmd/pastelocal-remote
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocal-linux-amd64 ./cmd/pastelocal
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocald-linux-amd64 ./cmd/pastelocald

build-linux-arm64:
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocal-remote-linux-arm64 ./cmd/pastelocal-remote
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocal-linux-arm64 ./cmd/pastelocal
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocald-linux-arm64 ./cmd/pastelocald

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocal-darwin-arm64 ./cmd/pastelocal
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocald-darwin-arm64 ./cmd/pastelocald
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocal-remote-darwin-arm64 ./cmd/pastelocal-remote

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocal-darwin-amd64 ./cmd/pastelocal
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocald-darwin-amd64 ./cmd/pastelocald
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/pastelocal-remote-darwin-amd64 ./cmd/pastelocal-remote

test:
	$(GO) test -v -race -cover ./internal/...

vet:
	$(GO) vet ./...

lint: vet
	golangci-lint run ./...
	gosec ./...

clean:
	rm -rf bin/ dist/

install: build
	install -m 0755 bin/pastelocal $(GOPATH)/bin/pastelocal
	install -m 0755 bin/pastelocald $(GOPATH)/bin/pastelocald

release:
	goreleaser release --clean

snapshot:
	goreleaser release --snapshot --clean

coverage:
	$(GO) test -coverprofile=coverage.out ./internal/...
	$(GO) tool cover -html=coverage.out -o coverage.html

e2e:
	$(GO) test -v -tags=e2e ./...
