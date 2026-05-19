BINARY := cymbal
GO_TAGS := sqlite_fts5

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG := github.com/1broseidon/cymbal/cmd
LDFLAGS := -X $(VERSION_PKG).version=$(VERSION) -X $(VERSION_PKG).commit=$(COMMIT) -X $(VERSION_PKG).date=$(DATE)
MODULE := $(shell go list -m)
COVER_PACKAGES := $(shell git ls-files '*.go' | while read file; do dirname "$$file"; done | sort -u | grep -v '^bench$$' | while read dir; do go list "./$$dir"; done | grep -v '^$(MODULE)$$')

.PHONY: build build-check ci clean install lint test test-coverage vulncheck

build:
	go build -tags "$(GO_TAGS)" -ldflags "$(LDFLAGS)" -o $(BINARY) .

build-check:
	go build -tags "$(GO_TAGS)" ./...

install:
	go install -tags "$(GO_TAGS)" -ldflags "$(LDFLAGS)" .

test:
	go test -tags "$(GO_TAGS)" ./...

test-coverage:
	go test -tags "$(GO_TAGS)" -covermode=atomic -coverprofile=coverage.txt $(COVER_PACKAGES)

lint:
	go vet ./...

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

ci: build-check lint test vulncheck

clean:
	rm -f $(BINARY) coverage.txt
