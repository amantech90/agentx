WAILS ?= $(shell go env GOPATH)/bin/wails

.PHONY: build build-macos test frontend

build:
	$(WAILS) build -clean -nocolour -o AgentX

build-macos:
	$(WAILS) build -clean -nocolour -o AgentX -compiler ./scripts/go-macos-wails.sh

test:
	go test ./...

frontend:
	$(MAKE) -C frontend build
