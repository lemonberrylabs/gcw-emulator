# Connector Hooks

Google Cloud connectors (`googleapis.*`, `gke.*`, …) call real GCP service
backends, which the emulator does not provide. Connector hooks let you map
each connector call name your workflows use to a **local handler** — an
executable or an HTTP endpoint — so the workflow source runs unmodified
against the emulator.

Because handlers can return structured errors, `try` / `retry` / `except`
around a hooked call behaves exactly as it would against the real service —
and you can inject faults on demand to exercise error paths that are hard to
reproduce in a real project.

## Configuration

Point the emulator at a hooks file:

```bash
gcw-emulator --connector-hooks=./hooks.yaml
# or
CONNECTOR_HOOKS=./hooks.yaml gcw-emulator
```

The file maps connector call names to handlers:

```yaml
connectors:
  googleapis.pubsub.v1.projects.topics.publish:
    exec: ./pubsub_publish.sh
  gke.create_job:
    http: http://gke-stub:9090/create_job
    timeout: 30s
```

Rules:

- Each hook sets exactly one of `exec` or `http`.
- Relative `exec` paths resolve against the directory containing the hooks
  file (so a mounted directory works the same inside Docker).
- Environment variables in `exec` and `http` values are expanded
  (`$VAR` / `${VAR}`).
- `timeout` is an optional Go duration bounding one invocation; the default
  is 1800s, matching the stdlib HTTP timeout, so long-polling connectors
  (e.g. `gke.await_job`) can block as long as they would for real.
- A hook name that matches a built-in function overrides it (a warning is
  logged).

## Handler contract

Every call sends the handler a JSON payload:

```json
{
  "connector": "googleapis.pubsub.v1.projects.topics.publish",
  "args": {
    "topic": "projects/my-project/topics/events",
    "body": { "messages": [ { "data": "aGVsbG8=" } ] }
  }
}
```

`args` is the call's `args` map evaluated (or `null` when the call has no
args).

### `exec` handlers

- The payload arrives on **stdin**; the connector name is also passed as
  `argv[1]` and as the `GCW_CONNECTOR` environment variable.
- **Exit 0**: stdout is parsed as JSON and becomes the call's result. Empty
  stdout means the call returns `null`. Stderr is logged for debugging.
- **Non-zero exit**: stderr may carry a structured error (see below).

```sh
#!/bin/sh
# pubsub_publish.sh — forward to the official Pub/Sub emulator
payload=$(cat)
topic=$(echo "$payload" | jq -r '.args.topic')
echo "$payload" | jq '{messages: .args.body.messages}' \
  | curl -sf -X POST -d @- "http://localhost:8085/v1/${topic}:publish" \
  || { echo '{"message": "pubsub emulator unreachable", "tags": ["ConnectionError"]}' >&2; exit 1; }
```

### `http` handlers

- The payload is POSTed as JSON (with an `X-GCW-Connector` header).
- **2xx**: the response body is parsed as JSON and becomes the call's result
  (empty body → `null`).
- **Non-2xx**: the body may carry a structured error; the error's `code`
  defaults to the HTTP status.

### Structured errors

A handler signals a workflow error with a JSON object:

```json
{ "message": "topic already exists", "code": 409, "tags": ["HttpError"] }
```

All fields are optional. The error is raised into the workflow as a regular
runtime error, so retry predicates and `except` blocks see `e.message`,
`e.code`, and `e.tags` exactly as they would from a real connector call. The
hooked connector name is attached as `e.connector`. Output that isn't a JSON
object becomes a generic error tagged `ConnectorHookError`, with the raw text
in the message.

A handler that times out raises a `TimeoutError`-tagged error; an `http`
handler that can't be reached raises `ConnectionFailedError` — both
catchable, like their real counterparts.

## Fault injection

Because the handler decides the outcome per call, failure testing is a
one-liner — for example, fail until the third attempt to exercise a retry
policy:

```sh
#!/bin/sh
count_file=/tmp/gke_create_job_attempts
n=$(( $(cat "$count_file" 2>/dev/null || echo 0) + 1 ))
echo "$n" > "$count_file"
if [ "$n" -lt 3 ]; then
  echo '{"message": "transient", "code": 503, "tags": ["HttpError"]}' >&2
  exit 1
fi
cat > /dev/null
echo '{"metadata": {"name": "job-1"}}'
```

## Docker Compose example

```yaml
services:
  workflows-emulator:
    image: ghcr.io/lemonberrylabs/gcw-emulator:latest
    environment:
      WORKFLOWS_DIR: /workflows
      CONNECTOR_HOOKS: /hooks/hooks.yaml
    volumes:
      - ./workflows:/workflows
      - ./hooks:/hooks
    ports:
      - "8787:8787"
      - "8788:8788"
```

Note that `exec` handlers run inside the emulator container, so scripts must
only rely on tools available there; use `http` handlers to delegate to a
sidecar container with richer tooling.

## See also

- [Limitations](../other/limitations.md) — what the emulator does not do
- [FAQ](../other/faq.md) — the environment-variable pattern, an alternative
  that avoids hooks by branching inside the workflow
