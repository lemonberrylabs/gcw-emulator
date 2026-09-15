# CLI & Configuration

## Starting the emulator

```bash
gcw-emulator
```

By default the emulator starts on port 8787 with no workflows loaded. Deploy workflows via the REST API.

### With a workflows directory

```bash
gcw-emulator --workflows-dir=./workflows --port=9090
```

This loads all `.yaml` and `.json` files from the directory and watches for changes.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8787` | HTTP server port |
| `HOST` | `0.0.0.0` | Bind address |
| `PROJECT` | `my-project` | GCP project ID for API paths |
| `LOCATION` | `us-central1` | GCP location for API paths |
| `CONNECTOR_HOOKS` | -- | Path to a [connector hooks](./connector-hooks.md) file mapping `googleapis.*`/`gke.*` call names to local handlers |

### Client-side variables

| Variable | Description |
|----------|-------------|
| `WORKFLOWS_EMULATOR_HOST` | Set in your app/tests to redirect workflow API calls to the emulator. Follows the standard `*_EMULATOR_HOST` convention used by other GCP emulators (Pub/Sub, Firestore, Spanner, etc.) |

Example:

```bash
export WORKFLOWS_EMULATOR_HOST=http://localhost:8787
go test ./...
```

## API Paths

All API paths include the project and location:

```
/v1/projects/{project}/locations/{location}/workflows/...
```

The `PROJECT` and `LOCATION` environment variables control these defaults. If you set `PROJECT=my-app` and `LOCATION=europe-west1`, the API paths become:

```
/v1/projects/my-app/locations/europe-west1/workflows/...
```

## API-Only Mode

When `--workflows-dir` is not set, the emulator starts with zero workflows. This is useful for integration tests where each test deploys its own workflow definition programmatically via the Workflows CRUD API.
