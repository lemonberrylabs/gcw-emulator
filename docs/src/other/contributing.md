# Contributing

Contributions are welcome. This guide covers the basics of contributing to the project.

## Getting started

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

## Building

```bash
go build ./cmd/gcw-emulator
```

## Running tests

### Unit tests

```bash
go test ./pkg/...
```

### Integration tests

Start the emulator:

```bash
go run ./cmd/gcw-emulator
```

In another terminal:

```bash
cd test/integration
WORKFLOWS_EMULATOR_HOST=http://localhost:8787 go test -v ./...
```

The integration test suite has 221 tests covering all step types, standard library functions, error handling, parallel execution, the REST API, the Web UI, and edge cases.

## Project structure

```
cmd/gcw-emulator/   Entry point (CLI)
pkg/
  api/              REST API handlers (Fiber)
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

## Code style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep functions focused and small
- Write tests for new functionality
- Error messages should be lowercase and descriptive

## Submitting changes

1. Ensure `go vet ./...` passes
2. Ensure all tests pass
3. Write a clear commit message describing the change
4. Submit a pull request against the `main` branch

## Reporting issues

Use [GitHub Issues](https://github.com/lemonberrylabs/gcw-emulator/issues) to report bugs or request features. Include:

- What you expected to happen
- What actually happened
- Steps to reproduce
- Workflow YAML (if applicable)
- Emulator version / commit hash
