.PHONY: all build test test-race lint fmt install clean

TAGS := fts5

all: build

build:
	go build -tags $(TAGS) -o poisk ./cmd/poisk

test:
	go test -tags $(TAGS) -timeout 5m ./...

test-race:
	go test -tags $(TAGS) -race -timeout 5m ./...

lint:
	golangci-lint run
	go mod tidy -diff
	go tool govulncheck ./...
	go tool deadcode -test ./...

fmt:
	golangci-lint fmt

install:
	go install -tags $(TAGS) ./cmd/poisk

clean:
	rm -f poisk
	go clean ./...
