BINARY := cymbal
CGO_CFLAGS := -DSQLITE_ENABLE_FTS5

.PHONY: build build-check ci clean install lint test vulncheck

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" go build -o $(BINARY) .

build-check:
	CGO_CFLAGS="$(CGO_CFLAGS)" go build ./...

install:
	CGO_CFLAGS="$(CGO_CFLAGS)" go install .

test:
	CGO_CFLAGS="$(CGO_CFLAGS)" go test ./...

lint:
	go vet ./...

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

ci: build-check lint test vulncheck

clean:
	rm -f $(BINARY)
