package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lemonberrylabs/gcw-emulator/pkg/stdlib"
	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

const testConnector = "googleapis.pubsub.v1.projects.topics.publish"

func requireUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("exec hook tests use /bin/sh scripts")
	}
}

// writeScript writes an executable shell script into dir and returns its path.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

// writeHooksFile writes a hooks YAML file into dir and returns its path.
func writeHooksFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "hooks.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write hooks file: %v", err)
	}
	return path
}

// callThroughRegistry registers the hooks and calls the connector the way the
// engine does: a single map argument.
func callThroughRegistry(t *testing.T, h *Hooks, connector string, args map[string]interface{}) (types.Value, error) {
	t.Helper()
	r := stdlib.NewRegistry()
	h.RegisterAll(r)

	callArgs := []types.Value{}
	if args != nil {
		callArgs = append(callArgs, types.ValueFromJSON(args))
	}
	return r.CallFunction(context.Background(), connector, callArgs)
}

func asWorkflowError(t *testing.T, err error) *types.WorkflowError {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	var we *types.WorkflowError
	if !errors.As(err, &we) {
		t.Fatalf("expected *types.WorkflowError, got %T: %v", err, err)
	}
	return we
}

func TestLoadValidation(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name    string
		content string
		wantErr string
	}{
		{"empty", "connectors: {}\n", "no connectors configured"},
		{"neither exec nor http", "connectors:\n  a.b:\n    timeout: 5s\n", "one of exec or http is required"},
		{"both exec and http", "connectors:\n  a.b:\n    exec: ./x.sh\n    http: http://localhost:1\n", "mutually exclusive"},
		{"bad timeout", "connectors:\n  a.b:\n    exec: ./x.sh\n    timeout: soon\n", "invalid timeout"},
		{"negative timeout", "connectors:\n  a.b:\n    exec: ./x.sh\n    timeout: -5s\n", "invalid timeout"},
		{"invalid yaml", "connectors: [not a map\n", "parse hooks file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeHooksFile(t, t.TempDir(), tc.content)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}

	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadResolvesRelativeExecAgainstHooksFileDir(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	writeScript(t, dir, "ok.sh", `echo '{"ok": true}'`)
	path := writeHooksFile(t, dir, "connectors:\n  a.b:\n    exec: ./ok.sh\n")

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Call from a different working directory to prove resolution is against
	// the hooks file, not the process cwd.
	result, err := callThroughRegistry(t, h, "a.b", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	ok, _ := result.AsMap().Get("ok")
	if !ok.AsBool() {
		t.Fatalf("expected {ok: true}, got %v", result)
	}
}

func TestLoadExpandsEnvVars(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	writeScript(t, dir, "ok.sh", `echo '{}'`)
	t.Setenv("GCW_TEST_HOOKS_DIR", dir)
	path := writeHooksFile(t, t.TempDir(), "connectors:\n  a.b:\n    exec: ${GCW_TEST_HOOKS_DIR}/ok.sh\n")

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := callThroughRegistry(t, h, "a.b", nil); err != nil {
		t.Fatalf("call: %v", err)
	}
}

func TestExecSuccessReturnsStdoutJSON(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	writeScript(t, dir, "publish.sh", `cat > /dev/null
echo '{"messageIds": ["local-1"]}'`)
	path := writeHooksFile(t, dir, fmt.Sprintf("connectors:\n  %s:\n    exec: ./publish.sh\n", testConnector))

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	result, err := callThroughRegistry(t, h, testConnector, map[string]interface{}{
		"topic": "projects/p/topics/t",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	ids, ok := result.AsMap().Get("messageIds")
	if !ok || ids.Type() != types.TypeList || ids.AsList()[0].AsString() != "local-1" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestExecReceivesPayloadOnStdin(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	// Echo the payload straight back as the result.
	writeScript(t, dir, "echo.sh", `cat`)
	path := writeHooksFile(t, dir, "connectors:\n  test.echo:\n    exec: ./echo.sh\n")

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	result, err := callThroughRegistry(t, h, "test.echo", map[string]interface{}{
		"topic": "projects/p/topics/t",
		"body":  map[string]interface{}{"messages": []interface{}{"m1"}},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	m := result.AsMap()
	if c, _ := m.Get("connector"); c.AsString() != "test.echo" {
		t.Fatalf("payload connector = %v", c)
	}
	args, _ := m.Get("args")
	topic, _ := args.AsMap().Get("topic")
	if topic.AsString() != "projects/p/topics/t" {
		t.Fatalf("payload args.topic = %v", topic)
	}
}

func TestExecNoArgsSendsNullArgs(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	writeScript(t, dir, "echo.sh", `cat`)
	path := writeHooksFile(t, dir, "connectors:\n  test.echo:\n    exec: ./echo.sh\n")

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	result, err := callThroughRegistry(t, h, "test.echo", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	args, ok := result.AsMap().Get("args")
	if !ok || !args.IsNull() {
		t.Fatalf("expected null args, got %v", args)
	}
}

func TestExecEmptyStdoutReturnsNull(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	writeScript(t, dir, "quiet.sh", `cat > /dev/null`)
	path := writeHooksFile(t, dir, "connectors:\n  test.quiet:\n    exec: ./quiet.sh\n")

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	result, err := callThroughRegistry(t, h, "test.quiet", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !result.IsNull() {
		t.Fatalf("expected null result, got %v", result)
	}
}

func TestExecInvalidJSONIsError(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	writeScript(t, dir, "bad.sh", `echo 'not json'`)
	path := writeHooksFile(t, dir, "connectors:\n  test.bad:\n    exec: ./bad.sh\n")

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = callThroughRegistry(t, h, "test.bad", nil)
	we := asWorkflowError(t, err)
	if !we.HasTag(TagConnectorHookError) || !strings.Contains(we.Message, "invalid JSON") {
		t.Fatalf("unexpected error: %v", we)
	}
}

func TestExecStructuredErrorDrivesWorkflowError(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	writeScript(t, dir, "conflict.sh", `echo '{"message": "topic already exists", "code": 409, "tags": ["HttpError"]}' >&2
exit 1`)
	path := writeHooksFile(t, dir, "connectors:\n  test.conflict:\n    exec: ./conflict.sh\n")

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = callThroughRegistry(t, h, "test.conflict", nil)
	we := asWorkflowError(t, err)
	if we.Message != "topic already exists" || we.Code != 409 || !we.HasTag(types.TagHttpError) {
		t.Fatalf("unexpected error: %+v", we)
	}
	if conn, ok := we.Extra["connector"]; !ok || conn.AsString() != "test.conflict" {
		t.Fatalf("expected connector in extra, got %+v", we.Extra)
	}
}

func TestExecUnstructuredErrorGetsDefaultTag(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	writeScript(t, dir, "boom.sh", `echo 'something broke' >&2
exit 3`)
	path := writeHooksFile(t, dir, "connectors:\n  test.boom:\n    exec: ./boom.sh\n")

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = callThroughRegistry(t, h, "test.boom", nil)
	we := asWorkflowError(t, err)
	if !we.HasTag(TagConnectorHookError) {
		t.Fatalf("expected %s tag, got %v", TagConnectorHookError, we.Tags)
	}
	if !strings.Contains(we.Message, "status 3") || !strings.Contains(we.Message, "something broke") {
		t.Fatalf("unexpected message: %q", we.Message)
	}
}

func TestExecTimeoutRaisesTimeoutError(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	// The backgrounded sleep inherits the stdout/stderr pipes and outlives the
	// killed shell — Wait must not block on it (cmd.WaitDelay).
	writeScript(t, dir, "slow.sh", "sleep 5 &\nsleep 5")
	path := writeHooksFile(t, dir, "connectors:\n  test.slow:\n    exec: ./slow.sh\n    timeout: 100ms\n")

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	start := time.Now()
	_, err = callThroughRegistry(t, h, "test.slow", nil)
	we := asWorkflowError(t, err)
	if !we.HasTag(types.TagTimeoutError) {
		t.Fatalf("expected TimeoutError tag, got %v", we.Tags)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout did not interrupt the hook")
	}
}

func TestExecMissingScriptIsError(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	path := writeHooksFile(t, dir, "connectors:\n  test.missing:\n    exec: ./nope.sh\n")

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = callThroughRegistry(t, h, "test.missing", nil)
	we := asWorkflowError(t, err)
	if !we.HasTag(TagConnectorHookError) || !strings.Contains(we.Message, "failed to start") {
		t.Fatalf("unexpected error: %v", we)
	}
}

func TestHTTPSuccess(t *testing.T) {
	var gotPayload map[string]interface{}
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-GCW-Connector")
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"messageIds": ["http-1"]}`)
	}))
	defer srv.Close()

	path := writeHooksFile(t, t.TempDir(), fmt.Sprintf("connectors:\n  %s:\n    http: %s\n", testConnector, srv.URL))
	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	result, err := callThroughRegistry(t, h, testConnector, map[string]interface{}{"topic": "t"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	ids, _ := result.AsMap().Get("messageIds")
	if ids.AsList()[0].AsString() != "http-1" {
		t.Fatalf("unexpected result: %v", result)
	}
	if gotHeader != testConnector {
		t.Fatalf("X-GCW-Connector = %q", gotHeader)
	}
	if gotPayload["connector"] != testConnector {
		t.Fatalf("payload connector = %v", gotPayload["connector"])
	}
}

func TestHTTPStructuredError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"message": "conflict", "tags": ["HttpError"]}`)
	}))
	defer srv.Close()

	path := writeHooksFile(t, t.TempDir(), fmt.Sprintf("connectors:\n  test.h:\n    http: %s\n", srv.URL))
	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = callThroughRegistry(t, h, "test.h", nil)
	we := asWorkflowError(t, err)
	// Code defaults to the HTTP status when the body doesn't set one.
	if we.Message != "conflict" || we.Code != 409 || !we.HasTag(types.TagHttpError) {
		t.Fatalf("unexpected error: %+v", we)
	}
}

func TestHTTPUnstructuredError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "kaboom")
	}))
	defer srv.Close()

	path := writeHooksFile(t, t.TempDir(), fmt.Sprintf("connectors:\n  test.h:\n    http: %s\n", srv.URL))
	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = callThroughRegistry(t, h, "test.h", nil)
	we := asWorkflowError(t, err)
	if we.Code != 500 || !we.HasTag(TagConnectorHookError) || !strings.Contains(we.Message, "kaboom") {
		t.Fatalf("unexpected error: %+v", we)
	}
}

func TestHTTPConnectionRefused(t *testing.T) {
	// Grab a port that nothing is listening on.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	path := writeHooksFile(t, t.TempDir(), fmt.Sprintf("connectors:\n  test.h:\n    http: %s\n", url))
	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = callThroughRegistry(t, h, "test.h", nil)
	we := asWorkflowError(t, err)
	if !we.HasTag(types.TagConnectionFailedError) {
		t.Fatalf("expected ConnectionFailedError tag, got %v", we.Tags)
	}
}

func TestRegisterAllOverridesExistingFunction(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	writeScript(t, dir, "sys.sh", `echo '"hooked"'`)
	path := writeHooksFile(t, dir, "connectors:\n  sys.get_env:\n    exec: ./sys.sh\n")

	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	result, err := callThroughRegistry(t, h, "sys.get_env", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if result.AsString() != "hooked" {
		t.Fatalf("hook did not override built-in: %v", result)
	}
}

func TestNamesSorted(t *testing.T) {
	path := writeHooksFile(t, t.TempDir(), "connectors:\n  z.b:\n    http: http://localhost:1\n  a.b:\n    http: http://localhost:1\n")
	h, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	names := h.Names()
	if len(names) != 2 || names[0] != "a.b" || names[1] != "z.b" || h.Len() != 2 {
		t.Fatalf("unexpected names: %v", names)
	}
}
