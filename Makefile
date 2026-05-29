BINARY := autoci

.PHONY: build run test fmt clean

build:
	go build -o bin/$(BINARY) ./cmd/autoci

run:
	go run ./cmd/autoci analyze --path ..

test:
	go test ./...

fmt:
	gofmt -w cmd internal

clean:
	rm -rf bin

