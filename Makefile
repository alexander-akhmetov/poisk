.PHONY: all build test test-race eval-test lint fmt install clean

TAGS := fts5

all: build

build:
	go build -tags $(TAGS) -o poisk ./cmd/poisk

test:
	go test -tags $(TAGS) -timeout 5m ./...

test-race:
	go test -tags $(TAGS) -race -timeout 5m ./...

eval-test:
	go test -tags '$(TAGS) eval' -timeout 5m -v -run TestEval ./internal/search/

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
