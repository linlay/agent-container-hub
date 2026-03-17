APP_NAME := agentboxd

.PHONY: build run test docker-build clean

build:
	go build -o $(APP_NAME) ./cmd/agentboxd

run:
	set -a; [ ! -f .env ] || . ./.env; set +a; go run ./cmd/agentboxd

test:
	go test ./...

docker-build:
	docker build -t agentbox:latest .

clean:
	rm -f $(APP_NAME)
