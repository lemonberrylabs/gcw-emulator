// Package api implements the REST API handlers matching the Google Cloud
// Workflows and Executions API surface.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gofiber/fiber/v2"
	"github.com/lemonberrylabs/gcw-emulator/pkg/ast"
	"github.com/lemonberrylabs/gcw-emulator/pkg/hooks"
	"github.com/lemonberrylabs/gcw-emulator/pkg/parser"
	"github.com/lemonberrylabs/gcw-emulator/pkg/runtime"
	"github.com/lemonberrylabs/gcw-emulator/pkg/stdlib"
	"github.com/lemonberrylabs/gcw-emulator/pkg/store"
	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// Server is the API server for the GCW emulator.
type Server struct {
	app     *fiber.App
	store   *store.Store
	parsed  map[string]*ast.Workflow // cached parsed workflows
	baseURL string                   // absolute base URL for callback endpoints
	hooks   *hooks.Hooks             // optional connector hooks

	mu      sync.RWMutex
	engines map[string]*runtime.Engine      // running execution engines (for cancel)
	cancels map[string]context.CancelFunc   // cancel functions for running executions
}

// New creates a new API server.
func New(s *store.Store) *Server {
	srv := &Server{
		store:   s,
		parsed:  make(map[string]*ast.Workflow),
		engines: make(map[string]*runtime.Engine),
		cancels: make(map[string]context.CancelFunc),
	}

	// Default base URL for callback endpoints; override with SetBaseURL.
	srv.baseURL = "http://localhost:8787"

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ReadTimeout:           30 * time.Second,
		WriteTimeout:          30 * time.Second,
	})

	// Workflows API
	app.Post("/v1/projects/:project/locations/:location/workflows", srv.createWorkflow)
	app.Get("/v1/projects/:project/locations/:location/workflows/:workflow", srv.getWorkflow)
	app.Get("/v1/projects/:project/locations/:location/workflows", srv.listWorkflows)
	app.Patch("/v1/projects/:project/locations/:location/workflows/:workflow", srv.updateWorkflow)
	app.Delete("/v1/projects/:project/locations/:location/workflows/:workflow", srv.deleteWorkflow)

	// Executions API
	app.Post("/v1/projects/:project/locations/:location/workflows/:workflow/executions", srv.createExecution)
	app.Get("/v1/projects/:project/locations/:location/workflows/:workflow/executions/:execution", srv.getExecution)
	app.Get("/v1/projects/:project/locations/:location/workflows/:workflow/executions", srv.listExecutions)
	app.Post("/v1/projects/:project/locations/:location/workflows/:workflow/executions/:execution\\:cancel", srv.cancelExecution)

	// Callbacks API
	app.Get("/v1/projects/:project/locations/:location/workflows/:workflow/executions/:execution/callbacks", srv.listCallbacks)
	app.All("/callbacks/:id", srv.sendCallback)

	srv.app = app
	return srv
}

// Listen starts the HTTP server on the given address.
func (s *Server) Listen(addr string) error {
	return s.app.Listen(addr)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown() error {
	return s.app.Shutdown()
}

// App returns the underlying Fiber app (useful for testing).
func (s *Server) App() *fiber.App {
	return s.app
}

// --- Workflow Handlers ---

type createWorkflowRequest struct {
	SourceContents string            `json:"sourceContents"`
	Description    string            `json:"description"`
	Labels         map[string]string `json:"labels"`
}

func (s *Server) createWorkflow(c *fiber.Ctx) error {
	parent := buildParent(c)
	workflowID := c.Query("workflowId")
	if workflowID == "" {
		return c.Status(400).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    400,
				"message": "workflowId query parameter is required",
				"status":  "INVALID_ARGUMENT",
			},
		})
	}

	var req createWorkflowRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    400,
				"message": fmt.Sprintf("invalid request body: %v", err),
				"status":  "INVALID_ARGUMENT",
			},
		})
	}

	if req.SourceContents == "" {
		return c.Status(400).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    400,
				"message": "sourceContents is required",
				"status":  "INVALID_ARGUMENT",
			},
		})
	}

	// Validate by parsing the workflow
	wfAST, err := parser.Parse([]byte(req.SourceContents))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    400,
				"message": fmt.Sprintf("invalid workflow definition: %v", err),
				"status":  "INVALID_ARGUMENT",
			},
		})
	}

	wf, err := s.store.CreateWorkflow(parent, workflowID, req.SourceContents, req.Description)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return c.Status(409).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    409,
					"message": err.Error(),
					"status":  "ALREADY_EXISTS",
				},
			})
		}
		return c.Status(500).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    500,
				"message": err.Error(),
				"status":  "INTERNAL",
			},
		})
	}

	// Cache the parsed workflow
	s.parsed[wf.Name] = wfAST

	// Return the workflow resource directly (emulator simplification -
	// real GCP returns a long-running operation, but we complete immediately)
	return c.Status(200).JSON(workflowToJSON(wf))
}

func (s *Server) getWorkflow(c *fiber.Ctx) error {
	name := buildWorkflowName(c)

	wf, err := s.store.GetWorkflow(name)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    404,
				"message": err.Error(),
				"status":  "NOT_FOUND",
			},
		})
	}

	return c.JSON(workflowToJSON(wf))
}

func (s *Server) listWorkflows(c *fiber.Ctx) error {
	parent := buildParent(c)
	workflows := s.store.ListWorkflows(parent)

	items := make([]fiber.Map, len(workflows))
	for i, wf := range workflows {
		items[i] = workflowToJSON(wf)
	}

	return c.JSON(fiber.Map{
		"workflows": items,
	})
}

func (s *Server) updateWorkflow(c *fiber.Ctx) error {
	name := buildWorkflowName(c)

	var req createWorkflowRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    400,
				"message": fmt.Sprintf("invalid request body: %v", err),
				"status":  "INVALID_ARGUMENT",
			},
		})
	}

	if req.SourceContents != "" {
		// Validate by parsing
		wfAST, err := parser.Parse([]byte(req.SourceContents))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    400,
					"message": fmt.Sprintf("invalid workflow definition: %v", err),
					"status":  "INVALID_ARGUMENT",
				},
			})
		}
		s.parsed[name] = wfAST
	}

	wf, err := s.store.UpdateWorkflow(name, req.SourceContents, req.Description)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    404,
				"message": err.Error(),
				"status":  "NOT_FOUND",
			},
		})
	}

	return c.JSON(fiber.Map{
		"name": fmt.Sprintf("projects/-/locations/-/operations/update-%s", c.Params("workflow")),
		"done": true,
		"response": workflowToJSON(wf),
	})
}

func (s *Server) deleteWorkflow(c *fiber.Ctx) error {
	name := buildWorkflowName(c)

	err := s.store.DeleteWorkflow(name)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    404,
				"message": err.Error(),
				"status":  "NOT_FOUND",
			},
		})
	}

	delete(s.parsed, name)

	return c.JSON(fiber.Map{
		"name": fmt.Sprintf("projects/-/locations/-/operations/delete-%s", c.Params("workflow")),
		"done": true,
	})
}

// --- Execution Handlers ---

type createExecutionRequest struct {
	Argument string `json:"argument"`
}

func (s *Server) createExecution(c *fiber.Ctx) error {
	workflowName := buildWorkflowName(c)

	var req createExecutionRequest
	if err := c.BodyParser(&req); err != nil && len(c.Body()) > 0 {
		return c.Status(400).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    400,
				"message": fmt.Sprintf("invalid request body: %v", err),
				"status":  "INVALID_ARGUMENT",
			},
		})
	}

	// Parse the argument JSON
	var args types.Value = types.Null
	if req.Argument != "" {
		var raw interface{}
		if err := json.Unmarshal([]byte(req.Argument), &raw); err != nil {
			return c.Status(400).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    400,
					"message": fmt.Sprintf("invalid argument JSON: %v", err),
					"status":  "INVALID_ARGUMENT",
				},
			})
		}
		args = types.ValueFromJSON(raw)
	}

	// Get parsed workflow
	wfAST, ok := s.parsed[workflowName]
	if !ok {
		// Try to parse from stored source
		wf, err := s.store.GetWorkflow(workflowName)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    404,
					"message": err.Error(),
					"status":  "NOT_FOUND",
				},
			})
		}
		parsed, err := parser.Parse([]byte(wf.SourceCode))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    500,
					"message": fmt.Sprintf("failed to parse workflow: %v", err),
					"status":  "INTERNAL",
				},
			})
		}
		wfAST = parsed
		s.parsed[workflowName] = wfAST
	}

	exec, err := s.store.CreateExecution(workflowName, args)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    500,
				"message": err.Error(),
				"status":  "INTERNAL",
			},
		})
	}

	// Execute the workflow asynchronously
	go s.runExecution(exec.Name, wfAST, args)

	return c.Status(200).JSON(executionToJSON(exec))
}

func (s *Server) runExecution(execName string, wfAST *ast.Workflow, args types.Value) {
	log.Printf("[DEBUG] Starting execution: %s", execName)

	funcs := stdlib.NewRegistry()
	funcs.RegisterHTTP(&http.Client{Timeout: stdlib.DefaultHTTPTimeout})
	funcs.RegisterWorkflowExecution(&storeAdapter{s.store}, s.parsed, s.childExecutor())
	if s.hooks != nil {
		s.hooks.RegisterAll(funcs)
	}

	engine := runtime.NewEngine(wfAST, funcs)
	ctx, cancel := context.WithCancel(stdlib.WithCallbackRegistrar(context.Background(), s, execName))

	// Store engine and cancel func for cancellation
	s.mu.Lock()
	s.engines[execName] = engine
	s.cancels[execName] = cancel
	s.mu.Unlock()

	result, err := engine.Execute(ctx, args)
	wasCancelled := engine.WasCancelled()

	s.mu.Lock()
	delete(s.engines, execName)
	delete(s.cancels, execName)
	s.mu.Unlock()
	cancel() // ensure resources are freed

	// Drop callback endpoints owned by this execution: nothing awaits them
	// any more, so later deliveries should 404 instead of succeeding.
	for _, id := range s.store.DeleteCallbacksForExecution(execName) {
		stdlib.GetCallbackStore().Delete(id)
	}

	if err != nil {
		if wasCancelled {
			log.Printf("[DEBUG] Execution %s cancelled", execName)
			_ = s.store.CancelExecution(execName)
		} else {
			log.Printf("[ERROR] Execution %s failed: %v", execName, err)
			_ = s.store.FailExecution(execName, err)
		}
	} else {
		log.Printf("[DEBUG] Execution %s completed successfully", execName)
		_ = s.store.CompleteExecution(execName, result)
	}
}

// childExecutor returns a ChildExecutor that creates a fresh engine for each
// child workflow execution, with all stdlib functions registered.
func (s *Server) childExecutor() stdlib.ChildExecutor {
	return func(ctx context.Context, wfAST *ast.Workflow, args types.Value) (types.Value, error) {
		funcs := stdlib.NewRegistry()
		funcs.RegisterHTTP(&http.Client{Timeout: stdlib.DefaultHTTPTimeout})
		funcs.RegisterWorkflowExecution(&storeAdapter{s.store}, s.parsed, s.childExecutor())
		if s.hooks != nil {
			s.hooks.RegisterAll(funcs)
		}

		engine := runtime.NewEngine(wfAST, funcs)
		return engine.Execute(ctx, args)
	}
}

// storeAdapter adapts *store.Store to the stdlib.WorkflowStore interface.
type storeAdapter struct {
	s *store.Store
}

func (a *storeAdapter) FindWorkflowByID(workflowID string) (stdlib.WorkflowInfo, error) {
	wf, err := a.s.FindWorkflowByID(workflowID)
	if err != nil {
		return stdlib.WorkflowInfo{}, err
	}
	return stdlib.WorkflowInfo{
		Name:       wf.Name,
		SourceCode: wf.SourceCode,
	}, nil
}

func (s *Server) getExecution(c *fiber.Ctx) error {
	name := buildExecutionName(c)

	exec, err := s.store.GetExecution(name)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    404,
				"message": err.Error(),
				"status":  "NOT_FOUND",
			},
		})
	}

	return c.JSON(executionToJSON(exec))
}

func (s *Server) listExecutions(c *fiber.Ctx) error {
	workflowName := buildWorkflowName(c)
	executions := s.store.ListExecutions(workflowName)

	items := make([]fiber.Map, len(executions))
	for i, exec := range executions {
		items[i] = executionToJSON(exec)
	}

	return c.JSON(fiber.Map{
		"executions": items,
	})
}

func (s *Server) cancelExecution(c *fiber.Ctx) error {
	name := buildExecutionName(c)

	// Cancel the engine if running
	s.mu.RLock()
	engine, hasEngine := s.engines[name]
	cancelFn, hasCancel := s.cancels[name]
	s.mu.RUnlock()
	if hasEngine {
		engine.Cancel()
	}
	if hasCancel {
		cancelFn()
	}

	err := s.store.CancelExecution(name)
	if err != nil {
		status := 404
		errStatus := "NOT_FOUND"
		if strings.Contains(err.Error(), "not active") {
			status = 400
			errStatus = "FAILED_PRECONDITION"
		}
		return c.Status(status).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    status,
				"message": err.Error(),
				"status":  errStatus,
			},
		})
	}

	exec, _ := s.store.GetExecution(name)
	return c.JSON(executionToJSON(exec))
}

// --- Callback Handlers ---

func (s *Server) listCallbacks(c *fiber.Ctx) error {
	execName := buildExecutionName(c)
	callbacks := s.store.ListCallbacks(execName)

	items := make([]fiber.Map, len(callbacks))
	for i, cb := range callbacks {
		items[i] = fiber.Map{
			"name":        cb.Name,
			"method":      cb.Method,
			"url":         cb.URL,
			"createTime":  cb.CreateTime.Format(time.RFC3339),
		}
	}

	return c.JSON(fiber.Map{
		"callbacks": items,
	})
}

// SetBaseURL sets the absolute base URL used to build callback endpoint URLs
// (e.g. "http://localhost:8787"). Configure it when the emulator is reachable
// under a different host, such as a Docker Compose service name.
func (s *Server) SetBaseURL(baseURL string) {
	s.baseURL = strings.TrimRight(baseURL, "/")
}

// SetConnectorHooks wires connector hooks into every execution started by
// this server. Hooked connector names become callable functions backed by
// the configured handlers.
func (s *Server) SetConnectorHooks(h *hooks.Hooks) {
	s.hooks = h
}

// RegisterCallback implements stdlib.CallbackRegistrar. It records the
// callback endpoint in the store so it is listed by the executions callbacks
// API, and returns the endpoint's absolute URL.
func (s *Server) RegisterCallback(executionName, callbackID, method string) string {
	url := s.baseURL + "/callbacks/" + callbackID
	s.store.CreateCallback(executionName, callbackID, method, url)
	return url
}

func (s *Server) sendCallback(c *fiber.Ctx) error {
	id := c.Params("id")
	url := s.baseURL + "/callbacks/" + id

	// Enforce the HTTP method chosen at events.create_callback_endpoint time,
	// as real GCW does.
	if cb, err := s.store.GetCallback(url); err == nil && !strings.EqualFold(cb.Method, c.Method()) {
		return c.Status(405).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    405,
				"message": fmt.Sprintf("callback '%s' only accepts method %s", id, cb.Method),
				"status":  "METHOD_NOT_ALLOWED",
			},
		})
	}

	var body interface{}
	if len(c.Body()) > 0 {
		if err := json.Unmarshal(c.Body(), &body); err != nil {
			return c.Status(400).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    400,
					"message": "invalid JSON body",
					"status":  "INVALID_ARGUMENT",
				},
			})
		}
	}

	headers := map[string]interface{}{}
	c.Request().Header.VisitAll(func(k, v []byte) {
		headers[string(k)] = string(v)
	})

	// Deliver in the shape real GCW passes to events.await_callback. Real GCW
	// passes the callback's absolute URL, so build it from the configured base
	// URL rather than echoing the request path.
	//
	// The payload outlives this handler (it is consumed by the workflow
	// goroutine), so every string in it must be an owned copy: fiber's
	// zero-copy ctx strings like c.Method() alias the connection's reusable
	// request buffer and are only valid until the handler returns.
	payload := map[string]interface{}{
		"received_time": time.Now().UTC().Format(time.RFC3339),
		"type":          "HTTP",
		"http_request": map[string]interface{}{
			"method":  strings.Clone(c.Method()),
			"url":     url,
			"headers": headers,
			"body":    body,
		},
	}

	if err := stdlib.GetCallbackStore().Deliver(id, types.ValueFromJSON(payload)); err != nil {
		return c.Status(404).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    404,
				"message": err.Error(),
				"status":  "NOT_FOUND",
			},
		})
	}
	return c.JSON(fiber.Map{})
}

// --- Directory Loading ---

var validWorkflowID = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// WatchDir loads all workflow files from the given directory and starts a
// background file watcher that hot-reloads on add/modify/delete.
func (s *Server) WatchDir(dir, project, location string) error {
	parent := fmt.Sprintf("projects/%s/locations/%s", project, location)

	// Initial load
	loaded, err := s.loadDir(dir, parent)
	if err != nil {
		return err
	}
	log.Printf("Loaded %d workflow(s) from %s", loaded, dir)

	// Start background watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating file watcher: %w", err)
	}
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return fmt.Errorf("watching directory %s: %w", dir, err)
	}

	go s.watchLoop(watcher, dir, parent)
	return nil
}

// loadDir does a one-shot load of all workflow files in dir.
func (s *Server) loadDir(dir, parent string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("reading workflows directory: %w", err)
	}

	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if s.deployFile(filepath.Join(dir, entry.Name()), parent) {
			loaded++
		}
	}
	return loaded, nil
}

// watchLoop processes fsnotify events with debouncing.
func (s *Server) watchLoop(watcher *fsnotify.Watcher, dir, parent string) {
	defer watcher.Close()

	// Debounce: collect events for 200ms before processing
	const debounce = 200 * time.Millisecond
	timer := time.NewTimer(debounce)
	timer.Stop()
	pending := make(map[string]fsnotify.Op)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			pending[event.Name] = event.Op
			timer.Reset(debounce)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[ERROR] File watcher error: %v", err)

		case <-timer.C:
			for path, op := range pending {
				s.handleFileEvent(path, op, parent)
			}
			pending = make(map[string]fsnotify.Op)
		}
	}
}

// handleFileEvent processes a single file change event.
func (s *Server) handleFileEvent(path string, op fsnotify.Op, parent string) {
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	if ext != ".yaml" && ext != ".yml" && ext != ".json" {
		return
	}

	workflowID := workflowIDFromFilename(name)
	if workflowID == "" {
		return
	}
	wfName := parent + "/workflows/" + workflowID

	// File removed
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Printf("[DEBUG] File deleted: %s — removing workflow %q", name, workflowID)
		if err := s.store.DeleteWorkflow(wfName); err != nil {
			log.Printf("Warning: could not delete workflow %q: %v", workflowID, err)
		}
		delete(s.parsed, wfName)
		return
	}

	// File added or modified
	log.Printf("[DEBUG] File changed: %s — reloading workflow %q", name, workflowID)
	s.deployFile(path, parent)
}

// deployFile reads, parses, and deploys a single workflow file. Returns true on success.
func (s *Server) deployFile(path, parent string) bool {
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	if ext != ".yaml" && ext != ".yml" && ext != ".json" {
		return false
	}

	workflowID := workflowIDFromFilename(name)
	if workflowID == "" {
		log.Printf("Warning: skipping file %q — invalid workflow ID", name)
		return false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Warning: could not read %q: %v", name, err)
		return false
	}

	wfAST, err := parser.Parse(data)
	if err != nil {
		log.Printf("Warning: could not parse %q: %v", name, err)
		return false
	}

	wfName := parent + "/workflows/" + workflowID

	// Try update first (file may already be deployed), fall back to create
	wf, err := s.store.UpdateWorkflow(wfName, string(data), "")
	if err != nil {
		wf, err = s.store.CreateWorkflow(parent, workflowID, string(data), "")
		if err != nil {
			log.Printf("Warning: could not deploy %q: %v", name, err)
			return false
		}
	}

	s.parsed[wf.Name] = wfAST
	log.Printf("Loaded workflow %q from %s", workflowID, name)
	return true
}

// workflowIDFromFilename extracts and validates a workflow ID from a filename.
func workflowIDFromFilename(name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	workflowID := strings.ToLower(base)

	if workflowID != base {
		log.Printf("Warning: lowercased workflow ID %q (from file %q)", workflowID, name)
	}

	if !validWorkflowID.MatchString(workflowID) || len(workflowID) > 128 {
		return ""
	}
	return workflowID
}

// --- Helpers ---

func buildParent(c *fiber.Ctx) string {
	return fmt.Sprintf("projects/%s/locations/%s", c.Params("project"), c.Params("location"))
}

func buildWorkflowName(c *fiber.Ctx) string {
	return fmt.Sprintf("projects/%s/locations/%s/workflows/%s",
		c.Params("project"), c.Params("location"), c.Params("workflow"))
}

func buildExecutionName(c *fiber.Ctx) string {
	return fmt.Sprintf("projects/%s/locations/%s/workflows/%s/executions/%s",
		c.Params("project"), c.Params("location"), c.Params("workflow"), c.Params("execution"))
}

func workflowToJSON(wf *store.Workflow) fiber.Map {
	return fiber.Map{
		"name":           wf.Name,
		"description":    wf.Description,
		"state":          wf.State,
		"revisionId":     wf.RevisionID,
		"createTime":     wf.CreateTime.Format(time.RFC3339),
		"updateTime":     wf.UpdateTime.Format(time.RFC3339),
		"sourceContents": wf.SourceCode,
	}
}

func executionToJSON(exec *store.Execution) fiber.Map {
	result := fiber.Map{
		"name":               exec.Name,
		"state":              exec.State,
		"startTime":          exec.StartTime.Format(time.RFC3339),
		"workflowRevisionId": exec.WorkflowRevisionID,
	}

	if exec.Argument != "" {
		result["argument"] = exec.Argument
	}
	if exec.Result != "" {
		result["result"] = exec.Result
	}
	if exec.Error != nil {
		result["error"] = fiber.Map{
			"payload": exec.Error.Payload,
			"context": exec.Error.Context,
		}
	}
	if !exec.EndTime.IsZero() {
		result["endTime"] = exec.EndTime.Format(time.RFC3339)
	}

	return result
}
