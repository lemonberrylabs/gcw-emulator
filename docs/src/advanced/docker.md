# Docker

## Run the emulator

```bash
docker run -p 8787:8787 -p 8788:8788 \
  ghcr.io/lemonberrylabs/gcw-emulator:latest
```

The image exposes port 8787 (REST API + Web UI) and port 8788 (gRPC).

## Mount a workflows directory

To use [directory watching](../guide/directory-watching.md) with Docker, mount your local workflows directory:

```bash
docker run -p 8787:8787 -p 8788:8788 \
  -v $(pwd)/workflows:/workflows \
  -e WORKFLOWS_DIR=/workflows \
  ghcr.io/lemonberrylabs/gcw-emulator:latest
```

The emulator watches for file changes and hot-reloads workflows automatically.

## Environment variables

Pass environment variables with `-e`:

```bash
docker run -p 8787:8787 \
  -e PROJECT=my-app \
  -e LOCATION=europe-west1 \
  -e PORT=8787 \
  -e GRPC_PORT=8788 \
  ghcr.io/lemonberrylabs/gcw-emulator:latest
```

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8787` | REST API and Web UI port |
| `HOST` | `0.0.0.0` | Bind address |
| `GRPC_PORT` | `8788` | gRPC API port |
| `PROJECT` | `my-project` | GCP project ID for API paths |
| `LOCATION` | `us-central1` | GCP location for API paths |
| `WORKFLOWS_DIR` | (none) | Path to workflows directory inside the container |

## Docker Compose

### Basic setup

```yaml
services:
  gcw-emulator:
    image: ghcr.io/lemonberrylabs/gcw-emulator:latest
    ports:
      - "8787:8787"
      - "8788:8788"
    volumes:
      - ./workflows:/workflows
    environment:
      - WORKFLOWS_DIR=/workflows
      - PROJECT=my-project
      - LOCATION=us-central1
```

### With your application services

A more complete example with the emulator orchestrating local services:

```yaml
services:
  gcw-emulator:
    image: ghcr.io/lemonberrylabs/gcw-emulator:latest
    ports:
      - "8787:8787"
    volumes:
      - ./workflows:/workflows
    environment:
      - WORKFLOWS_DIR=/workflows
    depends_on:
      - order-service
      - notification-service

  order-service:
    build: ./services/orders
    ports:
      - "9090:9090"

  notification-service:
    build: ./services/notifications
    ports:
      - "9091:9091"
```

In your workflow YAML, reference services by their Docker Compose service name:

```yaml
main:
  steps:
    - create_order:
        call: http.post
        args:
          url: http://order-service:9090/orders
          body:
            item: "widget"
        result: order
    - notify:
        call: http.post
        args:
          url: http://notification-service:9091/notify
          body:
            order_id: ${order.body.id}
```

When running inside Docker Compose, services communicate via the Docker network using service names as hostnames.

## Build your own image

The repository includes a multi-stage Dockerfile:

```bash
git clone https://github.com/lemonberrylabs/gcw-emulator.git
cd gcw-emulator
docker build -t gcw-emulator .
docker run -p 8787:8787 gcw-emulator
```

The Dockerfile uses a two-stage build: the first stage compiles the Go binary, and the second stage copies it into a minimal Alpine image with only `ca-certificates` and `tzdata`.

## Using with CI/CD

Run the emulator as a service in your CI pipeline:

```yaml
# GitHub Actions example
services:
  gcw-emulator:
    image: ghcr.io/lemonberrylabs/gcw-emulator:latest
    ports:
      - 8787:8787
```

Then your tests can deploy workflows and run executions against `http://localhost:8787` (or `http://gcw-emulator:8787` inside the service network).
