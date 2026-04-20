BINARY := cymbal
CGO_CFLAGS := -DSQLITE_ENABLE_FTS5

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG := github.com/1broseidon/cymbal/cmd
LDFLAGS := -X $(VERSION_PKG).version=$(VERSION) -X $(VERSION_PKG).commit=$(COMMIT) -X $(VERSION_PKG).date=$(DATE)

.PHONY: build build-check ci clean install lint test vulncheck

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

build-check:
	CGO_CFLAGS="$(CGO_CFLAGS)" go build ./...

install:
	CGO_CFLAGS="$(CGO_CFLAGS)" go install -ldflags "$(LDFLAGS)" .

test:
	CGO_CFLAGS="$(CGO_CFLAGS)" go test ./...

lint:
	go vet ./...

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

ci: build-check lint test vulncheck

clean:
	rm -f $(BINARY)
