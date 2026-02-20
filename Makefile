.PHONY: build test clean

build:
	go build -tags fts5 -o poisk ./cmd/poisk

test:
	go test -tags fts5 ./internal/...

clean:
	rm -f poisk
