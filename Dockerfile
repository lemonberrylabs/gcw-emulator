# Stage 1: Build the emulator binary
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /gcw-emulator ./cmd/gcw-emulator

# Stage 2: Minimal runtime image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /gcw-emulator /usr/local/bin/gcw-emulator

# Default environment
ENV HOST=0.0.0.0
ENV PORT=8787
ENV GRPC_PORT=8788
ENV PROJECT=my-project
ENV LOCATION=us-central1

# REST API
EXPOSE 8787
# gRPC
EXPOSE 8788

# Optional: mount a workflows directory here
VOLUME ["/workflows"]

ENTRYPOINT ["gcw-emulator"]
