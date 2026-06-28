package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/praful224/doktriai/doktriai-api"
	"github.com/praful224/doktriai/doktriai-core"
	"github.com/praful224/doktriai/doktriai-packages"
	kruntime "github.com/praful224/doktriai/doktriai-runtime"
)

func main() {
	addr := flag.String("addr", ":18080", "HTTP listen address")
	dataDir := flag.String("data-dir", "data", "state and audit storage directory")
	webDir := flag.String("web-dir", "web", "static web workspace directory")
	reconcileEvery := flag.Duration("reconcile-every", 5*time.Second, "reconciliation interval")
	policyPath := flag.String("policy", "doktri-policy.yaml", "Path to security policy YAML configuration file")
	flag.Parse()

	// Load security policy config
	if err := core.LoadPolicy(*policyPath); err != nil {
		log.Printf("Warning: Failed to load policy file %q: %v. Using defaults.", *policyPath, err)
	} else {
		log.Printf("Loaded security policy from %q successfully", *policyPath)
	}

	// Initialize OpenTelemetry distributed tracing
	shutdownTracer, err := core.InitTracer(context.Background(), "doktriai-api")
	if err != nil {
		log.Printf("Warning: Failed to initialize OpenTelemetry tracing: %v", err)
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = shutdownTracer(shutdownCtx)
		}()
	}

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	store, err := core.OpenStore(filepath.Join(*dataDir, "state.json"))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	bus := core.NewEventBus(200)
	driver := kruntime.NewDockerDriver("docker")
	engine := core.NewEngine(store, driver, bus, *reconcileEvery)
	server := api.NewServer(store, engine, bus, *webDir)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	engine.Start(ctx)
	bus.Publish(packages.Event{Level: "ok", Source: "api", Message: "DOKTRIAI API core cluster online"})

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("doktriai-api listening natively on %s", *addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http server structural failure: %v", err)
	}
}