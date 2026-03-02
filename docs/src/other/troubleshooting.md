# Troubleshooting

## Common issues

### "Connection refused" errors in workflow execution

**Symptom**: Workflow fails with `ConnectionFailedError` when calling `http.get` or `http.post`.

**Cause**: The target service is not running on the specified port.

**Fix**: Start your local service before executing the workflow. Verify it is listening on the expected port:

```bash
curl http://localhost:9090/health
```

### Workflow file not being picked up

**Symptom**: Added a YAML file to the workflows directory but it does not appear in the API.

**Possible causes**:
- Filename starts with a digit (e.g., `123-workflow.yaml`) -- must start with a letter
- Filename contains dots (e.g., `my.workflow.yaml`) -- dots produce invalid workflow IDs
- File extension is not `.yaml` or `.json`
- The `--workflows-dir` flag was not set when starting the emulator

### Execution stays in ACTIVE state

**Symptom**: `GET .../executions/{id}` keeps returning `"state": "ACTIVE"`.

**Possible causes**:
- The workflow is waiting on an `events.await_callback` step
- The workflow calls `sys.sleep` with a long duration
- An HTTP call step is hanging because the target service is slow to respond

### "workflow not found" when creating execution

**Symptom**: `POST .../executions` returns 404.

**Fix**: Verify the workflow was deployed successfully:

```bash
curl http://localhost:8787/v1/projects/my-project/locations/us-central1/workflows
```

Check that the workflow ID in the URL matches exactly.

### YAML parsing errors

**Symptom**: Create workflow returns 400 with "invalid workflow definition".

**Common YAML mistakes**:
- Expressions starting with `${` need to be quoted: `'${a + b}'` or `"${a + b}"`
- Indentation must be consistent (use spaces, not tabs)
- Lists use `- ` prefix with a space after the dash

### Results are JSON strings, not objects

**Symptom**: The `result` field in the execution response is a JSON-encoded string like `"\"hello\""` instead of `"hello"`.

**Explanation**: This matches the real GCW API. The `result` field is always a JSON-encoded string. Parse it in your client code:

```go
var result string
json.Unmarshal([]byte(exec.Result), &result)
```

## Getting help

- Check the [REST API Reference](../reference/rest-api.md) for correct endpoint formats
- Review the [Workflow Syntax](../reference/workflow-syntax.md) for YAML structure
- Open an issue at [github.com/lemonberrylabs/gcw-emulator/issues](https://github.com/lemonberrylabs/gcw-emulator/issues)
