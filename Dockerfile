# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

ARG SERVICE

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Contract bindings under internal/shared/eth/gen/ are .gitignored, so they
# must be regenerated here — the build cannot rely on the host having run
# `make gen-contracts`. Plain `go install` (no @version) builds abigen at the
# go-ethereum version pinned in go.mod.
RUN CGO_ENABLED=0 go install github.com/ethereum/go-ethereum/cmd/abigen && \
    mkdir -p internal/shared/eth/gen/conditionaltokens internal/shared/eth/gen/negriskadapter && \
    go generate ./internal/shared/eth/...

RUN CGO_ENABLED=0 GOOS=linux go build -o /app ./cmd/${SERVICE}

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && \
    wget -qO /bin/grpc_health_probe \
      https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.37/grpc_health_probe-linux-amd64 && \
    chmod +x /bin/grpc_health_probe

COPY --from=builder /app /app

ENTRYPOINT ["/app"]
