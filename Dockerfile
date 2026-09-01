# syntax=docker/dockerfile:1

########################
# Build stage
########################
FROM golang:1.24-bookworm AS builder

WORKDIR /src

# Cache go modules first
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the rest of the source
COPY . .

# All dependencies are pure Go, so build a statically linked binary.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/gopherai .

########################
# Runtime stage
########################
FROM debian:bookworm-slim

# Certificates for outbound HTTPS (AI/embedding APIs) + timezone data
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/gopherai /app/gopherai
# Baked-in default config; docker-compose overrides it with config.docker.toml
COPY config/config.toml /app/config/config.toml

# Runtime data directories (RAG uploads / docs)
RUN mkdir -p /app/uploads /app/docs

EXPOSE 9090

ENTRYPOINT ["/app/gopherai"]
