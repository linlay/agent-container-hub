FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/agent-container-hub ./cmd/agent-container-hub

FROM alpine:3.22

WORKDIR /app

COPY --from=builder /out/agent-container-hub /usr/local/bin/agent-container-hub
COPY .env.example /app/.env.example

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/agent-container-hub"]
