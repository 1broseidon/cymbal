BINARY := cymbal
CGO_CFLAGS := -DSQLITE_ENABLE_FTS5

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.12.1)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG := github.com/1broseidon/cymbal/cmd
LDFLAGS := -X $(VERSION_PKG).version=v0.12.1 -X $(VERSION_PKG).commit=$(COMMIT) -X $(VERSION_PKG).date=$(DATE)
MODULE := $(shell go list -m)
COVER_PACKAGES := $(shell git ls-files '*.go' | while read file; do dirname "$$file"; done | sort -u | while read dir; do go list "./$$dir"; done | grep -v '^$(MODULE)$$')

.PHONY: build build-check ci clean install lint test test-coverage vulncheck

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

build-check:
	CGO_CFLAGS="$(CGO_CFLAGS)" go build ./...

install:
	CGO_CFLAGS="$(CGO_CFLAGS)" go install -ldflags "$(LDFLAGS)" .

test:
	CGO_CFLAGS="$(CGO_CFLAGS)" go test ./...

test-coverage:
	CGO_CFLAGS="$(CGO_CFLAGS)" go test -covermode=atomic -coverprofile=coverage.txt $(COVER_PACKAGES)

lint:
	go vet ./...

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

ci: build-check lint test vulncheck

clean:
	rm -f $(BINARY) coverage.txt
