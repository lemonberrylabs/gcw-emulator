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
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/lemonberrylabs/gcw-emulator/pkg/ast"
	"github.com/lemonberrylabs/gcw-emulator/pkg/parser"
	"github.com/lemonberrylabs/gcw-emulator/pkg/runtime"
	"github.com/lemonberrylabs/gcw-emulator/pkg/stdlib"
	"github.com/lemonberrylabs/gcw-emulator/pkg/store"
	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// Server is the API server for the GCW emulator.
type Server struct {
	app    *fiber.App
	store  *store.Store
	parsed map[string]*ast.Workflow // cached parsed workflows
	engines map[string]*runtime.Engine // running execution engines (for cancel)
}

// New creates a new API server.
func New(s *store.Store) *Server {
	srv := &Server{
		store:   s,
		parsed:  make(map[string]*ast.Workflow),
		engines: make(map[string]*runtime.Engine),
	}

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
	app.Post("/callbacks/:id", srv.sendCallback)

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
	funcs := stdlib.NewRegistry()
	funcs.RegisterHTTP(&http.Client{Timeout: 30 * time.Second})

	engine := runtime.NewEngine(wfAST, funcs)

	// Store engine reference for cancellation
	s.engines[execName] = engine

	ctx := context.Background()
	result, err := engine.Execute(ctx, args)

	delete(s.engines, execName)

	if err != nil {
		_ = s.store.FailExecution(execName, err)
	} else {
		_ = s.store.CompleteExecution(execName, result)
	}
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
	if engine, ok := s.engines[name]; ok {
		engine.Cancel()
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

func (s *Server) sendCallback(c *fiber.Ctx) error {
	// Placeholder for callback handling
	return c.JSON(fiber.Map{
		"status": "ok",
	})
}

// --- Directory Loading ---

var validWorkflowID = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// WatchDir loads all .yaml and .json workflow files from the given directory
// and deploys them as workflows. File name (sans extension) becomes the workflow ID.
func (s *Server) WatchDir(dir, project, location string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading workflows directory: %w", err)
	}

	parent := fmt.Sprintf("projects/%s/locations/%s", project, location)
	loaded := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			continue
		}

		base := strings.TrimSuffix(name, ext)
		workflowID := strings.ToLower(base)

		if workflowID != base {
			log.Printf("Warning: lowercased workflow ID %q (from file %q)", workflowID, name)
		}

		if !validWorkflowID.MatchString(workflowID) || len(workflowID) > 128 {
			log.Printf("Warning: skipping file %q — invalid workflow ID %q", name, workflowID)
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			log.Printf("Warning: could not read %q: %v", name, err)
			continue
		}

		wfAST, err := parser.Parse(data)
		if err != nil {
			log.Printf("Warning: could not parse %q: %v", name, err)
			continue
		}

		wf, err := s.store.CreateWorkflow(parent, workflowID, string(data), "")
		if err != nil {
			log.Printf("Warning: could not deploy %q: %v", name, err)
			continue
		}

		s.parsed[wf.Name] = wfAST
		loaded++
		log.Printf("Loaded workflow %q from %s", workflowID, name)
	}

	log.Printf("Loaded %d workflow(s) from %s", loaded, dir)
	return nil
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
