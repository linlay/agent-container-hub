APP_NAME := agent-container-hub

.PHONY: build run test docker-build clean

build:
	go build -o $(APP_NAME) ./cmd/agent-container-hub

run:
	set -a; [ ! -f .env ] || . ./.env; set +a; go run ./cmd/agent-container-hub

test:
	go test ./...

docker-build:
	docker build -t agent-container-hub:latest .

clean:
	rm -f $(APP_NAME)
