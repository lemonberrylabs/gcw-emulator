// Package web provides the embedded web UI for the GCW emulator.
package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/lemonberrylabs/gcw-emulator/pkg/store"
)

//go:embed templates/*.html
var templateFS embed.FS

// Handler serves the web UI pages.
type Handler struct {
	store    *store.Store
	project  string
	location string
	funcMap  template.FuncMap
}

// pageData wraps all page-specific data with common fields.
type pageData struct {
	NavActive string
	Project   string
	Location  string
	Data      interface{}
}

// New creates a new web UI handler.
func New(s *store.Store, project, location string) *Handler {
	return &Handler{
		store:    s,
		project:  project,
		location: location,
		funcMap: template.FuncMap{
			"shortName":   shortName,
			"timeAgo":     timeAgo,
			"formatTime":  formatTime,
			"duration":    duration,
			"stateClass":  stateClass,
			"stateIcon":   stateIcon,
			"truncate":    truncate,
			"workflowID":  workflowID,
			"executionID": executionID,
			"countLines":  countLines,
			"hasPrefix":   strings.HasPrefix,
		},
	}
}

func (h *Handler) render(c *fiber.Ctx, page string, navActive string, data interface{}) error {
	// Parse templates fresh each time for the page-specific template
	// This avoids the Go template issue where define blocks conflict across pages
	tmpl := template.Must(
		template.New("").Funcs(h.funcMap).ParseFS(templateFS, "templates/layout.html", "templates/"+page),
	)

	pd := pageData{
		NavActive: navActive,
		Project:   h.project,
		Location:  h.location,
		Data:      data,
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, page, pd); err != nil {
		return c.Status(500).SendString(fmt.Sprintf("template error: %v", err))
	}

	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Send(buf.Bytes())
}

// Register adds web UI routes to the Fiber app.
func (h *Handler) Register(app *fiber.App) {
	app.Get("/ui", h.dashboard)
	app.Get("/ui/workflows", h.workflowList)
	app.Get("/ui/workflows/:id", h.workflowDetail)
	app.Get("/ui/workflows/:id/executions", h.executionList)
	app.Get("/ui/executions", h.allExecutionsList)
	app.Get("/ui/executions/:workflow/:execution", h.executionDetail)

	// Redirect root to UI
	app.Get("/", func(c *fiber.Ctx) error {
		return c.Redirect("/ui")
	})
}

// --- Page Data Types ---

type dashboardContent struct {
	Workflows      []*store.Workflow
	RecentExecs    []*executionView
	ActiveCount    int
	SucceededCount int
	FailedCount    int
	CancelledCount int
}

type executionView struct {
	*store.Execution
	WorkflowID string
	ExecID     string
}

type workflowListContent struct {
	Workflows []*workflowView
}

type workflowView struct {
	*store.Workflow
	ID             string
	ExecutionCount int
	ActiveCount    int
}

type workflowDetailContent struct {
	Workflow   *store.Workflow
	ID        string
	Executions []*executionView
}

type executionListContent struct {
	WorkflowID string
	Executions []*executionView
}

type executionDetailContent struct {
	Execution  *store.Execution
	WorkflowID string
	ExecID     string
}

type notFoundContent struct {
	Message string
}

// --- Page Handlers ---

func (h *Handler) dashboard(c *fiber.Ctx) error {
	parent := fmt.Sprintf("projects/%s/locations/%s", h.project, h.location)
	workflows := h.store.ListWorkflows(parent)

	sort.Slice(workflows, func(i, j int) bool {
		return workflows[i].UpdateTime.After(workflows[j].UpdateTime)
	})

	var allExecs []*executionView
	var active, succeeded, failed, cancelled int

	for _, wf := range workflows {
		execs := h.store.ListExecutions(wf.Name)
		for _, e := range execs {
			ev := &executionView{
				Execution:  e,
				WorkflowID: workflowID(wf.Name),
				ExecID:     executionID(e.Name),
			}
			allExecs = append(allExecs, ev)
			switch e.State {
			case store.ExecutionActive:
				active++
			case store.ExecutionSucceeded:
				succeeded++
			case store.ExecutionFailed:
				failed++
			case store.ExecutionCancelled:
				cancelled++
			}
		}
	}

	sort.Slice(allExecs, func(i, j int) bool {
		return allExecs[i].StartTime.After(allExecs[j].StartTime)
	})

	recent := allExecs
	if len(recent) > 10 {
		recent = recent[:10]
	}

	return h.render(c, "dashboard.html", "dashboard", dashboardContent{
		Workflows:      workflows,
		RecentExecs:    recent,
		ActiveCount:    active,
		SucceededCount: succeeded,
		FailedCount:    failed,
		CancelledCount: cancelled,
	})
}

func (h *Handler) workflowList(c *fiber.Ctx) error {
	parent := fmt.Sprintf("projects/%s/locations/%s", h.project, h.location)
	workflows := h.store.ListWorkflows(parent)

	sort.Slice(workflows, func(i, j int) bool {
		return workflows[i].UpdateTime.After(workflows[j].UpdateTime)
	})

	var views []*workflowView
	for _, wf := range workflows {
		execs := h.store.ListExecutions(wf.Name)
		activeCount := 0
		for _, e := range execs {
			if e.State == store.ExecutionActive {
				activeCount++
			}
		}
		views = append(views, &workflowView{
			Workflow:       wf,
			ID:             workflowID(wf.Name),
			ExecutionCount: len(execs),
			ActiveCount:    activeCount,
		})
	}

	return h.render(c, "workflow_list.html", "workflows", workflowListContent{
		Workflows: views,
	})
}

func (h *Handler) workflowDetail(c *fiber.Ctx) error {
	wfID := c.Params("id")
	name := fmt.Sprintf("projects/%s/locations/%s/workflows/%s", h.project, h.location, wfID)

	wf, err := h.store.GetWorkflow(name)
	if err != nil {
		return h.render(c, "not_found.html", "", notFoundContent{
			Message: fmt.Sprintf("Workflow '%s' not found", wfID),
		})
	}

	execs := h.store.ListExecutions(name)
	sort.Slice(execs, func(i, j int) bool {
		return execs[i].StartTime.After(execs[j].StartTime)
	})

	var execViews []*executionView
	for _, e := range execs {
		execViews = append(execViews, &executionView{
			Execution:  e,
			WorkflowID: wfID,
			ExecID:     executionID(e.Name),
		})
	}

	return h.render(c, "workflow_detail.html", "workflows", workflowDetailContent{
		Workflow:   wf,
		ID:        wfID,
		Executions: execViews,
	})
}

func (h *Handler) allExecutionsList(c *fiber.Ctx) error {
	parent := fmt.Sprintf("projects/%s/locations/%s", h.project, h.location)
	workflows := h.store.ListWorkflows(parent)

	var allViews []*executionView
	for _, wf := range workflows {
		execs := h.store.ListExecutions(wf.Name)
		for _, e := range execs {
			allViews = append(allViews, &executionView{
				Execution:  e,
				WorkflowID: workflowID(wf.Name),
				ExecID:     executionID(e.Name),
			})
		}
	}

	sort.Slice(allViews, func(i, j int) bool {
		return allViews[i].StartTime.After(allViews[j].StartTime)
	})

	return h.render(c, "execution_list.html", "executions", executionListContent{
		WorkflowID: "",
		Executions: allViews,
	})
}

func (h *Handler) executionList(c *fiber.Ctx) error {
	wfID := c.Params("id")
	name := fmt.Sprintf("projects/%s/locations/%s/workflows/%s", h.project, h.location, wfID)

	execs := h.store.ListExecutions(name)
	sort.Slice(execs, func(i, j int) bool {
		return execs[i].StartTime.After(execs[j].StartTime)
	})

	var views []*executionView
	for _, e := range execs {
		views = append(views, &executionView{
			Execution:  e,
			WorkflowID: wfID,
			ExecID:     executionID(e.Name),
		})
	}

	return h.render(c, "execution_list.html", "workflows", executionListContent{
		WorkflowID: wfID,
		Executions: views,
	})
}

func (h *Handler) executionDetail(c *fiber.Ctx) error {
	wfID := c.Params("workflow")
	execID := c.Params("execution")
	name := fmt.Sprintf("projects/%s/locations/%s/workflows/%s/executions/%s",
		h.project, h.location, wfID, execID)

	exec, err := h.store.GetExecution(name)
	if err != nil {
		return h.render(c, "not_found.html", "", notFoundContent{
			Message: fmt.Sprintf("Execution '%s' not found", execID),
		})
	}

	return h.render(c, "execution_detail.html", "workflows", executionDetailContent{
		Execution:  exec,
		WorkflowID: wfID,
		ExecID:     execID,
	})
}

// --- Template Helpers ---

func shortName(fullName string) string {
	parts := strings.Split(fullName, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return fullName
}

func workflowID(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		if p == "workflows" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return name
}

func executionID(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		if p == "executions" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return name
}

func timeAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02 15:04:05")
}

func duration(start, end time.Time) string {
	if end.IsZero() {
		d := time.Since(start)
		return fmt.Sprintf("%s (running)", formatDuration(d))
	}
	return formatDuration(end.Sub(start))
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", m, s)
}

func stateClass(state string) string {
	switch store.ExecutionState(state) {
	case store.ExecutionActive:
		return "state-active"
	case store.ExecutionSucceeded:
		return "state-succeeded"
	case store.ExecutionFailed:
		return "state-failed"
	case store.ExecutionCancelled:
		return "state-cancelled"
	default:
		return ""
	}
}

func stateIcon(state string) template.HTML {
	switch store.ExecutionState(state) {
	case store.ExecutionActive:
		return "&#9654;"
	case store.ExecutionSucceeded:
		return "&#10003;"
	case store.ExecutionFailed:
		return "&#10007;"
	case store.ExecutionCancelled:
		return "&#9632;"
	default:
		return "&#8226;"
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
