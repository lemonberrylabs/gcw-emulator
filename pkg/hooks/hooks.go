// Package hooks implements connector hooks: user-configured handlers that
// stand in for Google Cloud connector calls (e.g. googleapis.* and gke.*)
// when a workflow runs in the emulator. Each hook maps a connector call name
// to a local executable or an HTTP endpoint, so workflows that depend on
// connectors can execute locally without any changes to the workflow source.
//
// Handlers receive a JSON payload {"connector": <name>, "args": <call args>}
// and return the connector call's result as JSON. A handler signals a
// workflow error by exiting non-zero (exec) or responding non-2xx (http)
// with a JSON body {"message", "code", "tags"}, which is raised into the
// workflow as a regular error so try/retry/except semantics behave the same
// as against the real service.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/lemonberrylabs/gcw-emulator/pkg/stdlib"
	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// DefaultTimeout bounds a single hook invocation unless the hook configures
// its own timeout. It matches stdlib.DefaultHTTPTimeout so long-polling
// connectors (e.g. gke.await_job) behave the same through a hook.
const DefaultTimeout = stdlib.DefaultHTTPTimeout

// MaxOutputSize is the maximum size of a hook's result payload, matching
// stdlib.MaxHTTPResponseSize.
const MaxOutputSize = stdlib.MaxHTTPResponseSize

// TagConnectorHookError marks errors raised by a hook that did not specify
// its own tags.
const TagConnectorHookError = "ConnectorHookError"

// maxErrorTextSize caps how much raw handler output is copied into an error
// message when the handler did not produce a structured error.
const maxErrorTextSize = 4 * 1024

// HandlerConfig configures a single connector hook. Exactly one of Exec or
// HTTP must be set.
type HandlerConfig struct {
	// Exec is the path of an executable to run for each call. A relative
	// path resolves against the directory containing the hooks file.
	Exec string `yaml:"exec"`
	// HTTP is a URL the call payload is POSTed to.
	HTTP string `yaml:"http"`
	// Timeout is a Go duration string bounding one invocation
	// (default 1800s, matching the stdlib HTTP timeout).
	Timeout string `yaml:"timeout"`
}

// Config is the on-disk hooks file format:
//
//	connectors:
//	  googleapis.pubsub.v1.projects.topics.publish:
//	    exec: ./pubsub_publish.sh
//	  gke.create_job:
//	    http: http://gke-stub:9090/create_job
//	    timeout: 30s
type Config struct {
	Connectors map[string]HandlerConfig `yaml:"connectors"`
}

// handler executes one hook invocation.
type handler interface {
	call(ctx context.Context, connector string, payload []byte) (types.Value, error)
}

// Hooks is a set of loaded connector hooks ready to be registered into a
// stdlib function registry.
type Hooks struct {
	handlers map[string]handler
}

// Load reads and validates a hooks file. Environment variables in exec paths
// and http URLs are expanded ($VAR / ${VAR}).
func Load(path string) (*Hooks, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read hooks file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse hooks file %s: %w", path, err)
	}
	if len(cfg.Connectors) == 0 {
		return nil, fmt.Errorf("hooks file %s: no connectors configured", path)
	}

	baseDir := filepath.Dir(path)
	h := &Hooks{handlers: make(map[string]handler, len(cfg.Connectors))}
	for name, hc := range cfg.Connectors {
		timeout := DefaultTimeout
		if hc.Timeout != "" {
			d, err := time.ParseDuration(hc.Timeout)
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("hook %q: invalid timeout %q", name, hc.Timeout)
			}
			timeout = d
		}

		execPath := os.ExpandEnv(hc.Exec)
		httpURL := os.ExpandEnv(hc.HTTP)
		switch {
		case execPath != "" && httpURL != "":
			return nil, fmt.Errorf("hook %q: exec and http are mutually exclusive", name)
		case execPath != "":
			if !filepath.IsAbs(execPath) {
				execPath = filepath.Join(baseDir, execPath)
			}
			h.handlers[name] = &execHandler{path: execPath, timeout: timeout}
		case httpURL != "":
			h.handlers[name] = &httpHandler{url: httpURL, timeout: timeout, client: &http.Client{}}
		default:
			return nil, fmt.Errorf("hook %q: one of exec or http is required", name)
		}
	}
	return h, nil
}

// Names returns the hooked connector names, sorted.
func (h *Hooks) Names() []string {
	names := make([]string, 0, len(h.handlers))
	for name := range h.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Len returns the number of configured hooks.
func (h *Hooks) Len() int {
	return len(h.handlers)
}

// RegisterAll registers every hook as a callable function so workflow steps
// calling the hooked connector name invoke the handler.
func (h *Hooks) RegisterAll(r *stdlib.Registry) {
	for _, name := range h.Names() {
		if r.Has(name) {
			log.Printf("[WARN] connector hook %q overrides a built-in function", name)
		}
		name, hd := name, h.handlers[name]
		r.Register(name, func(ctx context.Context, args []types.Value) (types.Value, error) {
			return callHook(ctx, name, hd, args)
		})
	}
}

func callHook(ctx context.Context, connector string, hd handler, args []types.Value) (types.Value, error) {
	var argsGo interface{}
	if len(args) > 0 {
		argsGo = args[0].ToGoValue()
	}
	payload, err := json.Marshal(map[string]interface{}{
		"connector": connector,
		"args":      argsGo,
	})
	if err != nil {
		return types.Null, types.NewSystemError(fmt.Sprintf("hook for %s: encode payload: %v", connector, err))
	}
	return hd.call(ctx, connector, payload)
}

// --- Exec handler ---

type execHandler struct {
	path    string
	timeout time.Duration
}

func (e *execHandler) call(ctx context.Context, connector string, payload []byte) (types.Value, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.path, connector)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(), "GCW_CONNECTOR="+connector)
	// On timeout CommandContext kills only the immediate child; a grandchild
	// (e.g. a process spawned by a shell script) inherits the stdout/stderr
	// pipes and would block Wait until it exits. WaitDelay abandons the pipes
	// shortly after the kill so the hook's timeout is honored.
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return types.Null, hookTimeout(connector, e.timeout)
	case errors.Is(ctx.Err(), context.Canceled):
		// Execution was cancelled; propagate so the engine records a
		// cancellation, not a failure.
		return types.Null, ctx.Err()
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			fallback := fmt.Sprintf("hook for %s exited with status %d", connector, exitErr.ExitCode())
			return types.Null, hookError(connector, stderr.Bytes(), fallback)
		}
		return types.Null, &types.WorkflowError{
			Message: fmt.Sprintf("hook for %s failed to start: %v", connector, err),
			Tags:    []string{TagConnectorHookError, types.TagSystemError},
			Extra:   connectorExtra(connector),
		}
	}

	if stderr.Len() > 0 {
		log.Printf("[DEBUG] hook %s stderr: %s", connector, strings.TrimSpace(stderr.String()))
	}
	return parseResult(connector, stdout.Bytes())
}

// --- HTTP handler ---

type httpHandler struct {
	url     string
	timeout time.Duration
	client  *http.Client
}

func (hh *httpHandler) call(ctx context.Context, connector string, payload []byte) (types.Value, error) {
	ctx, cancel := context.WithTimeout(ctx, hh.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hh.url, bytes.NewReader(payload))
	if err != nil {
		return types.Null, types.NewSystemError(fmt.Sprintf("hook for %s: build request: %v", connector, err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GCW-Connector", connector)

	resp, err := hh.client.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
			return types.Null, hookTimeout(connector, hh.timeout)
		case errors.Is(ctx.Err(), context.Canceled):
			return types.Null, ctx.Err()
		}
		we := types.NewConnectionFailedError(fmt.Sprintf("hook for %s: %v", connector, err))
		we.Extra = connectorExtra(connector)
		return types.Null, we
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxOutputSize+1))
	if err != nil {
		we := types.NewConnectionError(fmt.Sprintf("hook for %s: read response: %v", connector, err))
		we.Extra = connectorExtra(connector)
		return types.Null, we
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fallback := fmt.Sprintf("hook for %s returned HTTP %d", connector, resp.StatusCode)
		e := hookError(connector, body, fallback)
		if e.Code == 0 {
			e.Code = int64(resp.StatusCode)
		}
		return types.Null, e
	}
	return parseResult(connector, body)
}

// --- Shared result/error handling ---

// parseResult converts a handler's output into the connector call's result.
// Empty output means the call returns null.
func parseResult(connector string, out []byte) (types.Value, error) {
	if len(out) > MaxOutputSize {
		return types.Null, &types.WorkflowError{
			Message: fmt.Sprintf("hook for %s: result exceeds %d bytes", connector, MaxOutputSize),
			Tags:    []string{TagConnectorHookError, types.TagResultSizeLimitExceededError},
			Extra:   connectorExtra(connector),
		}
	}
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return types.Null, nil
	}
	var raw interface{}
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return types.Null, &types.WorkflowError{
			Message: fmt.Sprintf("hook for %s returned invalid JSON: %v", connector, err),
			Tags:    []string{TagConnectorHookError},
			Extra:   connectorExtra(connector),
		}
	}
	return types.ValueFromJSON(raw), nil
}

// hookError builds the error raised into the workflow when a handler signals
// failure. A handler that produces a JSON object {"message", "code", "tags"}
// controls exactly what the workflow's try/retry/except sees; anything else
// becomes a generic ConnectorHookError carrying the raw text.
func hookError(connector string, body []byte, fallback string) *types.WorkflowError {
	e := &types.WorkflowError{
		Message: fallback,
		Tags:    []string{TagConnectorHookError},
		Extra:   connectorExtra(connector),
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return e
	}

	var structured struct {
		Message string   `json:"message"`
		Code    int64    `json:"code"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal(trimmed, &structured); err != nil ||
		(structured.Message == "" && structured.Code == 0 && len(structured.Tags) == 0) {
		text := trimmed
		if len(text) > maxErrorTextSize {
			text = text[:maxErrorTextSize]
		}
		e.Message = fallback + ": " + string(text)
		return e
	}

	if structured.Message != "" {
		e.Message = structured.Message
	}
	e.Code = structured.Code
	if len(structured.Tags) > 0 {
		e.Tags = structured.Tags
	}
	return e
}

func hookTimeout(connector string, timeout time.Duration) *types.WorkflowError {
	return &types.WorkflowError{
		Message: fmt.Sprintf("hook for %s timed out after %s", connector, timeout),
		Tags:    []string{types.TagTimeoutError, TagConnectorHookError},
		Extra:   connectorExtra(connector),
	}
}

func connectorExtra(connector string) map[string]types.Value {
	return map[string]types.Value{"connector": types.NewString(connector)}
}
