.PHONY: build build-all test lint vet clean install release

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -X main.version=$(VERSION) -s -w
GO = go
GOFLAGS = -trimpath

build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridge ./cmd/clipbridge
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridged ./cmd/clipbridged
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridge-remote ./cmd/clipbridge-remote

build-all: build-linux-amd64 build-linux-arm64 build-darwin-arm64 build-darwin-amd64

build-linux-amd64:
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridge-remote-linux-amd64 ./cmd/clipbridge-remote
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridge-linux-amd64 ./cmd/clipbridge
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridged-linux-amd64 ./cmd/clipbridged

build-linux-arm64:
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridge-remote-linux-arm64 ./cmd/clipbridge-remote
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridge-linux-arm64 ./cmd/clipbridge
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridged-linux-arm64 ./cmd/clipbridged

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridge-darwin-arm64 ./cmd/clipbridge
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridged-darwin-arm64 ./cmd/clipbridged

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridge-darwin-amd64 ./cmd/clipbridge
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/clipbridged-darwin-amd64 ./cmd/clipbridged

test:
	$(GO) test -v -race -cover ./internal/...

vet:
	$(GO) vet ./...

lint: vet
	golangci-lint run ./...
	gosec ./...

clean:
	rm -rf bin/

install: build
	install -m 0755 bin/clipbridge $(GOPATH)/bin/clipbridge
	install -m 0755 bin/clipbridged $(GOPATH)/bin/clipbridged

release:
	goreleaser release --clean

snapshot:
	goreleaser release --snapshot --clean

coverage:
	$(GO) test -coverprofile=coverage.out ./internal/...
	$(GO) tool cover -html=coverage.out -o coverage.html

e2e:
	$(GO) test -v -tags=e2e ./...
