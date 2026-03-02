# Installation

## Go install (recommended)

Requires Go 1.22 or later.

```bash
go install github.com/lemonberrylabs/gcw-emulator/cmd/gcw-emulator@latest
```

This installs the `gcw-emulator` binary to your `$GOPATH/bin` (or `$HOME/go/bin` by default).

Verify:

```bash
gcw-emulator --help
```

## Docker

Pull the pre-built image:

```bash
docker pull ghcr.io/lemonberrylabs/gcw-emulator:latest
```

Run it:

```bash
docker run -p 8787:8787 ghcr.io/lemonberrylabs/gcw-emulator:latest
```

See [Docker](../advanced/docker.md) for volume mounts, environment variables, and other usage details.

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
