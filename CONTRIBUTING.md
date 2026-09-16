# Contributing to GCW Emulator

Thank you for your interest in contributing! This document covers everything you need to get started.

## Quick Start

1. Fork the repository
2. Clone your fork:
   ```bash
   git clone https://github.com/YOUR_USERNAME/gcw-emulator.git
   cd gcw-emulator
   ```
3. Create a feature branch:
   ```bash
   git checkout -b feature/my-change
   ```
4. Make your changes with tests (see below)
5. Submit a pull request against `main`

## Test Requirements

**Every bug fix and feature must include tests.** PRs without adequate test coverage will not be merged.

- **Bug fixes**: Add a test that reproduces the bug and verifies the fix. If the bug is in runtime behavior, add an integration test. If it's in a specific package (parser, expression evaluator, etc.), add a unit test there too.
- **New features**: Add both unit tests for the package-level logic and integration tests that exercise the feature end-to-end through the REST API.
- **Refactors**: Ensure existing tests still pass. If you're changing behavior, update the tests to match.

### Unit Tests

Unit tests live alongside the code in `pkg/`:

```bash
go test ./pkg/...
```

### Integration Tests

Integration tests live in `test/integration/` and run against a live emulator instance. They use plain HTTP calls to test the full stack.

Start the emulator (the `CONNECTOR_HOOKS` file enables the connector hooks
tests, which skip when it is absent):

```bash
CONNECTOR_HOOKS=$PWD/test/integration/testdata/hooks/hooks.yaml go run ./cmd/gcw-emulator
```

In another terminal:

```bash
cd test/integration
WORKFLOWS_EMULATOR_HOST=http://localhost:8787 go test -v ./...
```

The integration test suite has 221+ tests covering all step types, the standard library, error handling, parallel execution, the REST API, the web UI, and edge cases. See `test/integration/helpers_test.go` for shared test utilities like `deployAndRun()`, `assertResultEquals()`, and `assertErrorHasTag()`.

### Writing Good Integration Tests

- Use `deployAndRun(t, yamlSource, args)` for simple workflow execution tests
- Use `assertResultEquals`, `assertResultContains`, `assertErrorContains`, `assertErrorHasTag` for assertions
- Each test should deploy its own workflow with a unique name (use `t.Name()`)
- Test both the happy path and error cases

## Building

```bash
go build ./cmd/gcw-emulator
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep functions focused and small
- Error messages should be lowercase and descriptive
- Run `go vet ./...` before submitting

## Project Structure

```
cmd/gcw-emulator/   Entry point (CLI)
pkg/
  api/              REST API handlers (Fiber)
  api/grpc/         gRPC server
  ast/              Workflow AST types
  expr/             Expression parser and evaluator
  parser/           YAML/JSON workflow parser
  runtime/          Workflow execution engine
  parallel/         Parallel execution support
  stdlib/           Standard library function registry
  store/            In-memory workflow and execution store
  types/            Value types and error types
web/                Web UI (Go templates)
test/integration/   Integration tests
  testdata/         Workflow YAML fixtures
docs/               Documentation (mdBook)
```

## Submitting Changes

1. Ensure `go vet ./...` passes
2. Ensure all unit tests pass: `go test ./pkg/...`
3. Ensure integration tests pass (see above)
4. Write a clear commit message describing the change
5. Submit a pull request against the `main` branch

## Reporting Issues

Use [GitHub Issues](https://github.com/lemonberrylabs/gcw-emulator/issues) to report bugs or request features. Include:

- What you expected to happen
- What actually happened
- Steps to reproduce
- Workflow YAML (if applicable)
- Emulator version or commit hash

## License

By contributing, you agree that your contributions will be licensed under the [Apache 2.0 License](LICENSE).
