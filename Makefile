BIN ?= bifrost
GO ?= go

.PHONY: test test-race vet build check

test:
	GOWORK=off $(GO) test ./...

test-race:
	GOWORK=off $(GO) test -race ./...

vet:
	GOWORK=off $(GO) vet ./...

build:
	GOWORK=off $(GO) build -o $(BIN) ./client/cmd/bifrost

check: test-race vet build
