# syntax=docker/dockerfile:1

########################
# Build stage
########################
FROM golang:1.24-bookworm AS builder

# onnxruntime_go binds to the ONNX Runtime C API via cgo, so a C toolchain is
# required to compile (the actual shared library is loaded lazily at runtime).
RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc libc6-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache go modules first
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the rest of the source
COPY . .

# CGO must be enabled for onnxruntime_go. Binary links against glibc, which is
# provided by the debian-slim runtime image below.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -trimpath -o /out/gopherai .

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
