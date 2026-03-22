APP_NAME := agent-container-hub
VERSION := $(shell cat VERSION 2>/dev/null || echo "dev")
ARCH := $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
LDFLAGS := -X main.buildVersion=$(VERSION)

.PHONY: build run test docker-build release clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(APP_NAME) ./cmd/agent-container-hub

run:
	set -a; [ ! -f .env ] || . ./.env; set +a; go run -ldflags "$(LDFLAGS)" ./cmd/agent-container-hub

test:
	go test ./...

docker-build:
	docker build --build-arg VERSION=$(VERSION) -t agent-container-hub:latest .

release:
	VERSION=$(VERSION) ARCH=$(ARCH) bash scripts/release.sh

clean:
	rm -f $(APP_NAME)
