// Package main is the entry point for the GCW emulator server.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lemonberrylabs/gcw-emulator/pkg/api"
	grpcapi "github.com/lemonberrylabs/gcw-emulator/pkg/api/grpc"
	"github.com/lemonberrylabs/gcw-emulator/pkg/store"
	"github.com/lemonberrylabs/gcw-emulator/web"
	"github.com/spf13/cobra"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "gcw-emulator",
	Short: "GCP Cloud Workflows Emulator",
	RunE:  run,
}

func init() {
	rootCmd.Version = version + " (commit=" + commit + ", built=" + date + ")"
	rootCmd.SetVersionTemplate("gcw-emulator version {{.Version}}\n")

	rootCmd.Flags().Int("port", 0, "HTTP server port (default 8787, env PORT)")
	rootCmd.Flags().Int("grpc-port", 0, "gRPC server port (default 8788, env GRPC_PORT)")
	rootCmd.Flags().String("host", "", "Bind address (default 0.0.0.0, env HOST)")
	rootCmd.Flags().String("project", "", "GCP project ID for API paths (default my-project, env PROJECT)")
	rootCmd.Flags().String("location", "", "GCP location for API paths (default us-central1, env LOCATION)")
	rootCmd.Flags().String("workflows-dir", "", "Directory of workflow YAML/JSON files to watch (env WORKFLOWS_DIR)")
	rootCmd.Flags().String("base-url", "", "Absolute base URL for callback endpoints, e.g. http://my-host:8787 (default http://localhost:<port>, env BASE_URL)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	port := envOrDefault("PORT", "8787")
	if v, _ := cmd.Flags().GetInt("port"); v != 0 {
		port = fmt.Sprintf("%d", v)
	}

	grpcPort := envOrDefault("GRPC_PORT", "8788")
	if v, _ := cmd.Flags().GetInt("grpc-port"); v != 0 {
		grpcPort = fmt.Sprintf("%d", v)
	}

	host := envOrDefault("HOST", "0.0.0.0")
	if v, _ := cmd.Flags().GetString("host"); v != "" {
		host = v
	}

	project := envOrDefault("PROJECT", "my-project")
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		project = v
	}

	location := envOrDefault("LOCATION", "us-central1")
	if v, _ := cmd.Flags().GetString("location"); v != "" {
		location = v
	}

	workflowsDir := os.Getenv("WORKFLOWS_DIR")
	if v, _ := cmd.Flags().GetString("workflows-dir"); v != "" {
		workflowsDir = v
	}

	addr := fmt.Sprintf("%s:%s", host, port)
	grpcAddr := fmt.Sprintf("%s:%s", host, grpcPort)

	baseURL := envOrDefault("BASE_URL", "http://localhost:"+port)
	if v, _ := cmd.Flags().GetString("base-url"); v != "" {
		baseURL = v
	}

	s := store.New()
	server := api.New(s)
	server.SetBaseURL(baseURL)

	// Load workflows from directory if specified
	if workflowsDir != "" {
		log.Printf("Watching workflows directory: %s", workflowsDir)
		if err := server.WatchDir(workflowsDir, project, location); err != nil {
			log.Printf("Warning: failed to watch workflows directory: %v", err)
		}
	}

	// Register the web UI (non-fatal if template parsing fails)
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Warning: web UI disabled due to template error: %v", r)
			}
		}()
		ui := web.New(s, project, location)
		ui.Register(server.App())
	}()

	// Start gRPC server. Callback endpoints created by gRPC-started
	// executions are registered through the REST server so they get
	// absolute URLs and are listed by the callbacks API.
	grpcServer := grpcapi.New(s)
	grpcServer.SetCallbackRegistrar(server)
	go func() {
		log.Printf("gRPC server listening on %s", grpcAddr)
		if err := grpcServer.Serve(grpcAddr); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down emulator...")
		grpcServer.GracefulStop()
		if err := server.Shutdown(); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}
	}()

	log.Printf("GCW Emulator %s (commit=%s, built=%s)", version, commit, date)
	log.Printf("Listening on %s (project=%s, location=%s)", addr, project, location)
	if workflowsDir != "" {
		log.Printf("Workflows directory: %s", workflowsDir)
	} else {
		log.Printf("API-only mode (no --workflows-dir specified)")
	}
	return server.Listen(addr)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
