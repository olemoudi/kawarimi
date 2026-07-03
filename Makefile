BINARY  := kawarimi
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/olemoudi/kawarimi/cmd.version=$(VERSION)

# Recipient platforms (kept in sync with cmd/package.go crossCompileTargets).
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: build test test-all vet fmt fmt-check cross install clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test -short ./...

test-all:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }

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
	go install -trimpath -ldflags "$(LDFLAGS)" .

clean:
	rm -f $(BINARY)
	rm -rf dist/
