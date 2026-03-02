<p align="center">
  <img src="assets/logo.png" alt="GCW Emulator" width="400">
</p>

<h1 align="center">GCP Cloud Workflows Emulator</h1>

<p align="center">
  A local emulator for <a href="https://cloud.google.com/workflows">Google Cloud Workflows</a> that lets you run and test workflows without deploying to GCP.
</p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white" alt="Go"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License"></a>
  <a href="https://github.com/lemonberrylabs/gcw-emulator/actions/workflows/ci.yml"><img src="https://github.com/lemonberrylabs/gcw-emulator/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

## Features

- **Full REST API compatibility** -- same endpoints, same request/response formats as Google Cloud Workflows and Executions APIs
- **All step types** -- assign, call, switch, for, parallel, try/except/retry, raise, return, next, steps
- **Expression engine** -- complete `${}` expression support with arithmetic, comparison, logical, and membership operators
- **Standard library** -- http, sys, text, json, base64, math, list, map, time, uuid, events, retry
- **Parallel execution** -- concurrent branches and parallel for loops with shared variables, concurrency limits, and exception policies
- **Error handling** -- try/except/retry with all 17 GCW error tags, exponential backoff, custom retry predicates
- **Directory watching** -- point at a directory of YAML files, get hot-reload on every save
- **Web UI** -- built-in dashboard at `/ui/` to inspect workflows and executions
- **Localhost orchestration** -- workflow `http.get/post/...` steps call your local services, just like real GCW calls Cloud Run
- **No GCP credentials required** -- runs fully offline

## Quick Start

### Install

```bash
go install github.com/lemonberrylabs/gcw-emulator/cmd/gcw-emulator@latest
```

### Create a workflow

Create a file `workflows/hello.yaml`:

```yaml
main:
  params: [args]
  steps:
    - greet:
        assign:
          - greeting: '${"Hello, " + args.name + "!"}'
    - done:
        return: ${greeting}
```

### Start the emulator

```bash
gcw-emulator --workflows-dir=./workflows
```

The emulator starts on port **8787** by default (REST) and **8788** (gRPC). Edit a workflow file and it's redeployed automatically.

### Run a workflow

```bash
# Start an execution
curl -s -X POST http://localhost:8787/v1/projects/my-project/locations/us-central1/workflows/hello/executions \
  -H "Content-Type: application/json" \
  -d '{"argument": "{\"name\": \"Alice\"}"}'

# Check the result (replace <execution-id> from the response above)
curl -s http://localhost:8787/v1/projects/my-project/locations/us-central1/workflows/hello/executions/<execution-id>
```

### Open the Web UI

Browse to [http://localhost:8787/ui/](http://localhost:8787/ui/) to see deployed workflows, trigger executions, and inspect results.

## Usage

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8787` | HTTP/REST server port |
| `GRPC_PORT` | `8788` | gRPC server port |
| `HOST` | `0.0.0.0` | Bind address |
| `PROJECT` | `my-project` | GCP project ID used in API paths |
| `LOCATION` | `us-central1` | GCP location used in API paths |
| `WORKFLOWS_DIR` | -- | Directory of workflow YAML/JSON files to watch |
| `WORKFLOWS_EMULATOR_HOST` | -- | Set this in your app/tests to point clients at the emulator (follows the `*_EMULATOR_HOST` convention used by Pub/Sub, Firestore, etc.) |

### CLI Flags

All environment variables can also be set via CLI flags:

```bash
gcw-emulator --port=9090 --grpc-port=9091 --project=my-project --location=us-central1 --workflows-dir=./workflows
```

CLI flags take precedence over environment variables.

### Directory Watching

When started with `--workflows-dir`, the emulator:

1. Reads all `.yaml` and `.json` files in the directory
2. Deploys each file as a workflow (filename without extension = workflow ID)
3. Watches for changes and hot-reloads modified workflows
4. Running executions are not affected -- they continue using the definition they started with

**Workflow ID rules**: lowercase letters, digits, hyphens, underscores. Must start with a letter. Max 128 characters. Uppercase filenames are lowercased automatically. Files with invalid names are skipped with a warning.

### API-Only Mode

If you omit `--workflows-dir`, the emulator starts with zero workflows. Deploy workflows programmatically via the REST API -- useful for test setups where each test deploys its own workflow definition.

## Integration Testing (Go)

The emulator is designed for Go integration tests. Start the emulator as a separate process (or in a `TestMain`), then use plain HTTP calls against it:

```go
package myservice_test

import (
    "bytes"
    "encoding/json"
    "net/http"
    "os"
    "testing"
    "time"
)

var emulatorURL string

func TestMain(m *testing.M) {
    emulatorURL = os.Getenv("WORKFLOWS_EMULATOR_HOST")
    if emulatorURL == "" {
        emulatorURL = "http://localhost:8787"
    }
    os.Exit(m.Run())
}

func TestMyWorkflow(t *testing.T) {
    // 1. Deploy a workflow
    body, _ := json.Marshal(map[string]string{
        "sourceContents": `
main:
  steps:
    - call_service:
        call: http.get
        args:
          url: http://localhost:9090/api/data
        result: response
    - done:
        return: ${response.body}
`,
    })
    url := emulatorURL + "/v1/projects/my-project/locations/us-central1/workflows?workflowId=test-wf"
    resp, err := http.Post(url, "application/json", bytes.NewReader(body))
    if err != nil {
        t.Fatal(err)
    }
    resp.Body.Close()

    // 2. Execute the workflow
    execURL := emulatorURL + "/v1/projects/my-project/locations/us-central1/workflows/test-wf/executions"
    resp, err = http.Post(execURL, "application/json", nil)
    if err != nil {
        t.Fatal(err)
    }
    var exec map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&exec)
    resp.Body.Close()

    // 3. Poll for completion
    execName := exec["name"].(string)
    getURL := emulatorURL + "/v1/" + execName
    for i := 0; i < 50; i++ {
        resp, _ = http.Get(getURL)
        json.NewDecoder(resp.Body).Decode(&exec)
        resp.Body.Close()
        if exec["state"] == "SUCCEEDED" || exec["state"] == "FAILED" {
            break
        }
        time.Sleep(100 * time.Millisecond)
    }

    if exec["state"] != "SUCCEEDED" {
        t.Fatalf("workflow failed: %v", exec["error"])
    }
    t.Logf("result: %s", exec["result"])
}
```

This pattern lets you test that your workflow correctly orchestrates your local services.

## REST API Reference

All paths are prefixed with `/v1/projects/{project}/locations/{location}`.

### Workflows

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/workflows?workflowId={id}` | Create a workflow |
| `GET` | `/workflows/{id}` | Get a workflow |
| `GET` | `/workflows` | List workflows |
| `PATCH` | `/workflows/{id}` | Update a workflow |
| `DELETE` | `/workflows/{id}` | Delete a workflow |

**Create/Update request body:**
```json
{
  "sourceContents": "main:\n  steps:\n    - done:\n        return: 42\n"
}
```

### Executions

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/workflows/{id}/executions` | Start an execution |
| `GET` | `/workflows/{id}/executions/{execId}` | Get execution status/result |
| `GET` | `/workflows/{id}/executions` | List executions |
| `POST` | `/workflows/{id}/executions/{execId}:cancel` | Cancel a running execution |

**Create execution request body:**
```json
{
  "argument": "{\"key\": \"value\"}"
}
```

The `argument` field is a JSON-encoded string (matching the real GCW API).

### Callbacks

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/workflows/{id}/executions/{execId}/callbacks` | List callbacks |
| `POST` | `/callbacks/{callbackId}` | Send a callback |

### Execution States

- `ACTIVE` -- currently running
- `SUCCEEDED` -- completed successfully (check `result` field)
- `FAILED` -- completed with error (check `error` field)
- `CANCELLED` -- cancelled via API

## Web UI

The built-in web UI is served at `http://localhost:8787/ui/` (or your configured port) and provides:

| Page | Path | Description |
|------|------|-------------|
| Dashboard | `/ui` | Overview of workflows and recent executions |
| Workflow List | `/ui/workflows` | All deployed workflows with execution counts |
| Workflow Detail | `/ui/workflows/{id}` | Workflow source and execution history |
| Execution List | `/ui/executions` | All executions across workflows |
| Execution Detail | `/ui/executions/{workflowId}/{execId}` | Execution result, error, and arguments |

## Docker

```bash
docker run -p 8787:8787 ghcr.io/lemonberrylabs/gcw-emulator:latest
```

With a local workflows directory:

```bash
docker run -p 8787:8787 \
  -v $(pwd)/workflows:/workflows \
  -e WORKFLOWS_DIR=/workflows \
  ghcr.io/lemonberrylabs/gcw-emulator:latest
```

## Supported Features

### Step Types

- [x] `assign` -- variable assignment (up to 50 per step)
- [x] `call` -- HTTP calls, stdlib calls, subworkflow calls
- [x] `switch` -- conditional branching (up to 50 conditions)
- [x] `for` -- iteration over lists, maps, and ranges
- [x] `parallel` -- concurrent branches and parallel for loops
- [x] `try/except/retry` -- error handling with retry policies
- [x] `raise` -- throw errors (string or map)
- [x] `return` -- return values from workflows/subworkflows
- [x] `next` -- jump to steps, `end`, `break`, `continue`
- [x] `steps` -- nested step grouping

### Standard Library

- [x] `http.get`, `http.post`, `http.put`, `http.patch`, `http.delete`, `http.request`
- [x] `sys.get_env`, `sys.log`, `sys.now`, `sys.sleep`
- [x] `text.find_all`, `text.find_all_regex`, `text.replace_all`, `text.replace_all_regex`, `text.split`, `text.substring`, `text.to_lower`, `text.to_upper`, `text.url_encode`, `text.url_decode`, `text.match_regex`
- [x] `json.decode`, `json.encode`, `json.encode_to_string`
- [x] `base64.decode`, `base64.encode`
- [x] `math.abs`, `math.floor`, `math.max`, `math.min`
- [x] `list.concat`, `list.prepend`
- [x] `map.get`, `map.delete`, `map.merge`, `map.merge_nested`
- [x] `uuid.generate`
- [x] `events.create_callback_endpoint`, `events.await_callback`
- [x] Built-in functions: `default`, `keys`, `len`, `type`, `int`, `double`, `string`, `bool`
- [x] Retry policies: `http.default_retry`, `http.default_retry_non_idempotent`, `retry.always`, `retry.never`

### Expression Engine

- [x] Arithmetic: `+`, `-`, `*`, `/`, `%`, `//`
- [x] Comparison: `==`, `!=`, `<`, `>`, `<=`, `>=`
- [x] Logical: `and`, `or`, `not`
- [x] Membership: `in`
- [x] Property access: dot notation, bracket notation
- [x] Type system: int, double, string, bool, null, list, map

### Parallel Execution

- [x] Parallel branches (up to 10)
- [x] Parallel for loops
- [x] Shared variables with atomic read/write
- [x] Concurrency limit (max 20)
- [x] Exception policies: `unhandled` and `continueAll`
- [x] Nesting depth limit: 2

### Error Handling

- [x] All 17 error tags: AuthError, ConnectionError, ConnectionFailedError, HttpError, IndexError, KeyError, OperationError, ParallelNestingError, RecursionError, ResourceLimitError, ResponseTypeError, SystemError, TimeoutError, TypeError, UnhandledBranchError, ValueError, ZeroDivisionError
- [x] Exponential backoff retry
- [x] Custom retry predicates (subworkflow-based)

### Limits

- [x] 50 assignments per assign step
- [x] 50 conditions per switch
- [x] 20 max call stack depth
- [x] 10 branches per parallel step
- [x] 2 max parallel nesting depth
- [x] 20 max concurrent branches/iterations
- [x] 100,000 max steps per execution

## FAQ

See the [FAQ](https://lemonberrylabs.github.io/gcw-emulator/other/faq.html) for common questions, including how to work around unsupported Google Cloud native functions (e.g., Secret Manager) using environment variables and conditional execution.

## Limitations

The following are **not** supported:

- **Google Cloud Connectors** (`googleapis.*`) -- the emulator handles HTTP calls but not connector-specific semantics. Mock connectors by running local HTTP services.
- **IAM / Authentication** -- the emulator accepts all requests without credentials.
- **Eventarc / Pub/Sub triggers** -- executions are triggered via REST API only.
- **Long-running Operations** -- workflow CRUD operations return immediately instead of returning a polling operation.
- **Workflow execution step history** -- execution results are available, but step-by-step audit logs are not.
- **CMEK / Billing / Quotas** -- not relevant for local development.
- **Multi-region semantics** -- single local instance.

## Contributing

Contributions are welcome.

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-change`)
3. Write tests for your changes
4. Ensure all tests pass: `go test ./...`
5. Submit a pull request

### Running Integration Tests

Start the emulator:

```bash
go run ./cmd/gcw-emulator
```

In another terminal:

```bash
cd test/integration
WORKFLOWS_EMULATOR_HOST=http://localhost:8787 go test -v ./...
```

### Project Structure

```
cmd/gcw-emulator/   Entry point (CLI)
pkg/api/            REST API handlers
pkg/ast/            Workflow AST types
pkg/expr/           Expression parser and evaluator
pkg/parser/         YAML/JSON workflow parser
pkg/runtime/        Workflow execution engine
pkg/parallel/       Parallel execution support
pkg/stdlib/         Standard library functions
pkg/store/          In-memory workflow and execution store
pkg/types/          Value types and error types
web/                Web UI (Go templates)
test/integration/   Integration test suite (221 tests)
```

## License

Apache 2.0 -- see [LICENSE](LICENSE) for details.
