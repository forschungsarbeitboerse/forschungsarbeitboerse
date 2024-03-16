VERSION := $(shell git describe --tags --always --dirty)

.PHONY: all
all: build

.PHONY: build
build:
	CGO_ENABLED=1 go build \
		-ldflags "-X main.Version=$(VERSION)" \
		-o bin/forschungsarbeitboerse \
		.

.PHONY: dev
dev:
	air server --host 127.0.0.1 --port 4444
