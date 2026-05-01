BINARY := contaigen
CMD := ./cmd/contaigen
DIST := dist
TOOLS_DIR := .bin
GORELEASER_VERSION ?= v2.15.4
GORELEASER_BIN := $(TOOLS_DIR)/goreleaser
GORELEASER ?= $(GORELEASER_BIN)
GORELEASER_PREREQ := $(if $(filter $(GORELEASER_BIN),$(GORELEASER)),$(GORELEASER_BIN),)

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build clean integration-test release-check snapshot test

$(GORELEASER_BIN):
	mkdir -p $(TOOLS_DIR)
	GOBIN=$(abspath $(TOOLS_DIR)) go install github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)

build:
	mkdir -p $(DIST)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(CMD)

test:
	go test ./...

integration-test:
	CONTAIGEN_INTEGRATION=1 go test ./internal/dockerx -run Integration -count=1

release-check: $(GORELEASER_PREREQ)
	$(GORELEASER) check

snapshot: $(GORELEASER_PREREQ)
	$(GORELEASER) release --snapshot --clean

clean:
	rm -rf $(DIST)
