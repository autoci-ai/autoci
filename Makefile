BINARY := autoci
VERSION ?= 0.1.0-dev
LDFLAGS := -X github.com/autoci-ai/autoci/internal/buildinfo.Version=$(VERSION)

.PHONY: build run test fmt clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/autoci

run:
	go run ./cmd/autoci analyze --path ..

test:
	go test ./...

fmt:
	gofmt -w cmd internal

clean:
	rm -rf bin
