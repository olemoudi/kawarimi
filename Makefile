BINARY  := kawarimi
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/olemoudi/kawarimi/cmd.version=$(VERSION)

# Recipient platforms (kept in sync with cmd/package.go crossCompileTargets).
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: build test test-all vet fmt fmt-check cross install clean

# Own code only (exclude the vendored tree from gofmt).
GOFILES := $(shell git ls-files '*.go' | grep -v '^vendor/')

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test -short ./...

test-all:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w $(GOFILES)

fmt-check:
	@test -z "$$(gofmt -l $(GOFILES))" || { echo "gofmt needed:"; gofmt -l $(GOFILES); exit 1; }

# Cross-compile the recipient binaries into dist/.
cross:
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ "$$os" = windows ] && ext=.exe; \
		out=dist/$(BINARY)-$$os-$$arch$$ext; \
		echo "building $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$out . || exit 1; \
	done

install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" .

clean:
	rm -f $(BINARY)
	rm -rf dist/
