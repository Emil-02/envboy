.PHONY: build test race lint install fuzz

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o envboy .

test:
	go test ./...

race:
	go test -race ./...

lint:
	golangci-lint run ./...

fuzz:
	go test ./internal/dotenv -run '^$$' -fuzz FuzzParse -fuzztime 30s

install:
	go install -ldflags "-s -w -X main.version=$(VERSION)" .
