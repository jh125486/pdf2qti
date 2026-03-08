.PHONY: build test lint clean install

BINARY := pdf2qti
CMD     := ./cmd/pdf2qti

build:
	go build -o bin/$(BINARY) $(CMD)

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ out/

install:
	go install $(CMD)

.DEFAULT_GOAL := build
