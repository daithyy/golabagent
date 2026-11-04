package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/daithyy/golabagent/api"
	"github.com/daithyy/golabagent/cfg"
	"github.com/daithyy/golabagent/llm"
	"github.com/daithyy/golabagent/mcpserver"
	"github.com/daithyy/golabagent/rag"
	"github.com/daithyy/golabagent/store"
	"github.com/daithyy/golabagent/tools"
	"github.com/daithyy/golabagent/workflows"
)

const version = "v0.1.0"

func main() {
	showHelp := flag.Bool("help", false, "show help information")
	showVersion := flag.Bool("version", false, "show version information")
	mcpMode := flag.Bool("mcp", false, "serve the agent's tools over MCP instead of running HTTP API")
	envFile := flag.String("env", ".env", "path to the .env file")

	flag.Usage = func() {
		log.Printf("usage: %s [options]\n", os.Args[0])
		log.Println("\ngolabagent - Lab Insight Agent")
		log.Println("\noptions:")
		flag.PrintDefaults()
		log.Println("\nexamples:")
		log.Printf("  %s                           # Run the HTTP API and Temporal worker\n", os.Args[0])
		log.Printf("  %s -mcp -env /path/to/.env   # Run as an MCP server over stdio\n", os.Args[0])
		log.Printf("  %s -help                     # Show this help\n", os.Args[0])
	}

	flag.Parse()

	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

	if *showVersion {
		log.Printf("golabagent %s\n", version)
		log.Println("Lab Insight Agent")
		os.Exit(0)
	}

	if err := cfg.Parse(*envFile); err != nil {
		log.Fatalf("error parsing configuration: %v", err)
	}
	c := cfg.Config

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Open(ctx, c.PostgresDSN())
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	if err := store.Migrate(ctx, pool); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	portfolioSchema, err := store.DescribeSchema(ctx, pool, "uw")
	if err != nil {
		log.Fatalf("failed to read portfolio schema: %v", err)
	}

	model := llm.New(c.OllamaURL(), c.ChatModel, c.EmbeddingModel, c.ChatThink, c.ChatNumCtx)
	indexer := rag.NewIndexer(pool, model)
	toolbox := tools.New(pool, indexer, portfolioSchema)

	if *mcpMode {
		log.Println("serving MCP over stdio")
		if err := mcpserver.Run(ctx, mcpserver.New(toolbox, version)); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatalf("mcp server: %v", err)
		}
		return
	}

	temporal, err := client.Dial(client.Options{HostPort: c.TemporalAddress(), Namespace: c.TemporalNamespace})
	if err != nil {
		log.Fatalf("failed to connect to temporal: %v", err)
	}
	defer temporal.Close()

	w := worker.New(temporal, c.TemporalTaskQueue, worker.Options{})
	w.RegisterWorkflow(workflows.AskWorkflow)
	w.RegisterWorkflow(workflows.IngestWorkflow)
	w.RegisterWorkflow(workflows.ImportWorkflow)
	w.RegisterActivity(&workflows.Activities{
		LLM:     model,
		Tools:   toolbox,
		Indexer: indexer,
		Pool:    pool,
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	})
	if err := w.Start(); err != nil {
		log.Fatalf("failed to start temporal worker: %v", err)
	}
	defer w.Stop()

	srv := &http.Server{
		Addr:    ":" + c.ServerPort,
		Handler: api.Setup(&api.Handlers{Temporal: temporal, TaskQueue: c.TemporalTaskQueue, Indexer: indexer}),
	}

	go func() {
		<-ctx.Done()
		log.Println("shutting down gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on :%s (chat model %s, embedding model %s)", c.ServerPort, c.ChatModel, c.EmbeddingModel)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("failed to start server: %v", err)
	}
}
