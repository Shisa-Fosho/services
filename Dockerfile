# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

ARG SERVICE

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app ./cmd/${SERVICE}

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && \
    wget -qO /bin/grpc_health_probe \
      https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.37/grpc_health_probe-linux-amd64 && \
    chmod +x /bin/grpc_health_probe

COPY --from=builder /app /app

ENTRYPOINT ["/app"]
