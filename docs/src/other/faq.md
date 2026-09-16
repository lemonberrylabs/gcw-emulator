# FAQ

## How do I handle Google Cloud native functions (e.g., Secret Manager) that the emulator doesn't support?

The emulator does not support `googleapis.*` connectors such as `googleapis.secretmanager.v1.projects.secrets.versions.access`. These require real GCP service backends.

The primary mechanism is [connector hooks](../guide/connector-hooks.md): map each connector call name to a local script or HTTP endpoint, and the workflow source runs unmodified.

An alternative that needs no emulator configuration is **environment variables with conditional execution**: inject the value via `sys.get_env` at the emulator level, and only call the GCP-native function in production when the env var is not set.

### Pattern

**1. Read the value from an env var with a default in `init`:**

```yaml
- init:
    assign:
      - projectId: '${sys.get_env("PROJECT_ID")}'
      - mySecret: '${sys.get_env("MY_SECRET", "")}'
```

`sys.get_env(name, default)` accepts a second argument -- if the env var isn't set, it returns the default (empty string here) instead of raising an error.

**2. Wrap the GCP-native call in a `switch` so it only runs when the env var is empty:**

```yaml
- maybe_get_secret:
    switch:
      - condition: ${mySecret == ""}
        steps:
          - fetch_secret:
              call: googleapis.secretmanager.v1.projects.secrets.versions.access
              args:
                name: '${"projects/" + projectId + "/secrets/MY_SECRET/versions/latest"}'
              result: secretResult
          - extract_secret:
              assign:
                - mySecret: ${text.decode(base64.decode(secretResult.payload.data))}
```

If `mySecret` is already populated from the env var, the `switch` falls through and execution continues with the existing value. If it's empty (i.e., running in production without the env var), it fetches from Secret Manager.

### How it works in each environment

| Environment | `MY_SECRET` env var | Behavior |
|-------------|---------------------|----------|
| **Emulator** | Set to the actual secret value | `switch` falls through, uses env var directly |
| **GCP Production** | Not set | `switch` enters the branch, calls Secret Manager |

This pattern works for any `googleapis.*` connector call -- not just Secret Manager. The key idea is: provide the value via an environment variable when running locally, and let the workflow fetch it from GCP when the env var is absent.

### See also

- [Limitations](./limitations.md) -- full list of unsupported features
- [CLI & Configuration](../guide/configuration.md) -- environment variable reference
