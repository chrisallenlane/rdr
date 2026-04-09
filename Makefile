BINARY    := rdr
BUILD_DIR := ./bin
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS   := -s -w -X main.version=$(VERSION)

.PHONY: build run test fuzz lint fmt vet a11y clean docker docker-multiarch release help

## Show available commands
help:
	@awk '/^## /{desc=substr($$0,4)} /^[a-z][-a-z]*:/{if(desc){sub(/:.*$$/,"",$$1); printf "  \033[36m%-12s\033[0m %s\n",$$1,desc; desc=""}}' $(MAKEFILE_LIST)

## Build for current platform
build:
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/rdr

## Build and run
run: build
	$(BUILD_DIR)/$(BINARY)

## Run all tests
test:
	go test ./... -v -count=1

## Run fuzz tests (10s each by default; override with FUZZ_TIME=30s)
FUZZ_TIME ?= 10s
fuzz:
	go test ./internal/sanitize/...  -fuzz=FuzzHTML                -fuzztime=$(FUZZ_TIME)
	go test ./internal/sanitize/...  -fuzz=FuzzResolveRelativeURLs -fuzztime=$(FUZZ_TIME)
	go test ./internal/sanitize/...  -fuzz=FuzzHighlightCodeBlocks -fuzztime=$(FUZZ_TIME)
	go test ./internal/database/...  -fuzz=FuzzParseVersion        -fuzztime=$(FUZZ_TIME)
	go test ./internal/handler/...   -fuzz=FuzzHandleImportOPML    -fuzztime=$(FUZZ_TIME)
	go test ./internal/handler/...   -fuzz=FuzzCollectFeedOutlines -fuzztime=$(FUZZ_TIME)

## Run linters (golangci-lint must be installed separately)
lint:
	golangci-lint run ./...
	npx eslint static/js/

## Format code
fmt:
	gofmt -s -w .
	npx prettier --write 'static/js/**/*.js'

## Run go vet
vet:
	go vet ./...

## Run accessibility audit (requires running server; override with A11Y_URL=…)
A11Y_URL ?= http://localhost:8080
a11y:
	npx pa11y $(A11Y_URL)/login
	npx pa11y $(A11Y_URL)/register

## Build Docker image
docker:
	docker build --build-arg VERSION=$(VERSION) -t $(BINARY) .

## Build multi-arch Docker image (amd64 + arm64)
docker-multiarch:
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=$(VERSION) -t $(BINARY) .

## Remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

## Build for all release platforms
release: clean
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64   ./cmd/rdr
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64   ./cmd/rdr
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-amd64  ./cmd/rdr
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-arm64  ./cmd/rdr
