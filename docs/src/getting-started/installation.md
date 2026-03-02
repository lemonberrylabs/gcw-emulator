# Installation

## Docker (easiest)

Pull and run the pre-built multi-arch image (amd64/arm64):

```bash
docker run -p 8787:8787 -p 8788:8788 ghcr.io/lemonberrylabs/gcw-emulator:latest
```

With a local workflows directory (hot-reloads on every save):

```bash
docker run -p 8787:8787 -p 8788:8788 \
  -v $(pwd)/workflows:/workflows \
  -e WORKFLOWS_DIR=/workflows \
  ghcr.io/lemonberrylabs/gcw-emulator:latest
```

See [Docker](../advanced/docker.md) for Docker Compose examples, CI/CD setup, environment variables, and building your own image.

## Go install

Requires Go 1.25 or later.

```bash
go install github.com/lemonberrylabs/gcw-emulator/cmd/gcw-emulator@latest
```

This installs the `gcw-emulator` binary to your `$GOPATH/bin` (or `$HOME/go/bin` by default).

Start the emulator:

```bash
gcw-emulator --workflows-dir=./workflows
```

## Build from source

```bash
git clone https://github.com/lemonberrylabs/gcw-emulator.git
cd gcw-emulator
go build -o gcw-emulator ./cmd/gcw-emulator
./gcw-emulator
```

## Verify the installation

Once the emulator is running, confirm it responds:

```bash
curl http://localhost:8787/v1/projects/my-project/locations/us-central1/workflows
```

Expected response:

```json
{"workflows": []}
```

Open the Web UI at [http://localhost:8787/ui/](http://localhost:8787/ui/) to see the dashboard.

## Ports

| Port | Protocol | Description |
|------|----------|-------------|
| 8787 | HTTP | REST API and Web UI (configurable via `PORT` env var) |
| 8788 | gRPC | gRPC API (configurable via `GRPC_PORT` env var) |

## Next steps

Proceed to the [Quick Start](./quick-start.md) to deploy and run your first workflow.
