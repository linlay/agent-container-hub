FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/agentboxd ./cmd/agentboxd

FROM alpine:3.22

WORKDIR /app

COPY --from=builder /out/agentboxd /usr/local/bin/agentboxd
COPY .env.example /app/.env.example

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/agentboxd"]
