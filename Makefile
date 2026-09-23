BINARY := cymbal
CGO_CFLAGS := -DSQLITE_ENABLE_FTS5

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
MODULE := github.com/1broseidon/cymbal
VERSION_PKG := $(MODULE)/cmd
LDFLAGS := -X $(VERSION_PKG).version=$(VERSION) -X $(VERSION_PKG).commit=$(COMMIT) -X $(VERSION_PKG).date=$(DATE)

.PHONY: build build-check ci clean install lint test test-coverage vulncheck

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

build-check:
	CGO_CFLAGS="$(CGO_CFLAGS)" go build ./...

install:
	CGO_CFLAGS="$(CGO_CFLAGS)" go install -ldflags "$(LDFLAGS)" .

test:
	CGO_CFLAGS="$(CGO_CFLAGS)" go test ./...

# Coverage leaves out bench/, the evaluation harness, and the root package,
# which is only main.go and has no tests. Drop the root exclusion if it gets some.
test-coverage:
	CGO_CFLAGS="$(CGO_CFLAGS)" go test -covermode=atomic -coverprofile=coverage.txt \
		$$(go list ./... | grep -vx -e '$(MODULE)' -e '$(MODULE)/bench')

lint:
	go vet ./...

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

ci: build-check lint test vulncheck

clean:
	rm -f $(BINARY) coverage.txt
