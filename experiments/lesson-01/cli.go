package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if err := runCLI(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printUsage()
		return nil
	}
	switch args[0] {
	case "index":
		if len(args) < 2 {
			return fmt.Errorf("usage: codepilot index <repository>")
		}
		return runIndex(ctx, args[1])
	case "ask":
		if len(args) < 3 {
			return fmt.Errorf("usage: codepilot ask <repository> <question>")
		}
		return runAsk(ctx, args[1], strings.Join(args[2:], " "))
	case "agent":
		if len(args) < 3 {
			return fmt.Errorf("usage: codepilot agent <repository> <goal>")
		}
		return runAgent(ctx, args[1], strings.Join(args[2:], " "))
	case "tool":
		if len(args) < 3 {
			return fmt.Errorf("usage: codepilot tool <repository> <tool_name> [args_json]")
		}
		argsJSON := "{}"
		if len(args) >= 4 {
			argsJSON = strings.Join(args[3:], " ")
		}
		return runTool(ctx, args[1], args[2], argsJSON)
	case "eval":
		if len(args) < 2 {
			return fmt.Errorf("usage: codepilot eval <repository>")
		}
		return runEval(ctx, args[1])
	case "mcp":
		if len(args) < 2 {
			return fmt.Errorf("usage: codepilot mcp <repository>")
		}
		return runMCP(ctx, args[1])
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage() {
	fmt.Print(`CodePilot - AI-Powered Repository-Aware Software Engineering Assistant

Usage:
  codepilot index <repository>                 Incrementally index a repository
  codepilot ask <repository> <question>        Ask a source-grounded question
  codepilot agent <repository> <goal>          Run autonomous multi-step reasoning agent
  codepilot tool <repository> <name> [args]    Directly run a deterministic intelligence tool
  codepilot eval <repository>                  Run retrieval evaluation & regression gate
  codepilot mcp <repository>                   Start stdio Model Context Protocol (MCP) server
  codepilot help                               Show this help message
`)
}

func newCLIIndexer(store IndexStore, config IndexConfig) *IncrementalIndexer {
	return &IncrementalIndexer{
		Config:   config,
		Embedder: NewEmbeddingClient("http://localhost:11434", config.EmbeddingModel),
		Hasher:   &SHA256FileHasher{},
		Store:    store,
		Scanner:  &RepositoryScanner{Options: config.scannerOptions()},
	}
}

func runIndex(ctx context.Context, root string) error {
	store, err := NewFilesystemIndexStore("")
	if err != nil {
		return err
	}
	indexer := newCLIIndexer(store, DefaultIndexConfig())
	indexed, err := indexer.Index(ctx, root)
	if err != nil {
		return err
	}

	fmt.Printf("Indexed %s successfully.\n", indexed.Root)
	fmt.Println(indexed.Metrics.String())
	return nil
}

func runAsk(ctx context.Context, root, question string) error {
	_ = ctx
	store, err := NewFilesystemIndexStore("")
	if err != nil {
		return err
	}
	config := DefaultIndexConfig()
	indexer := newCLIIndexer(store, config)
	indexed, err := indexer.Load(context.Background(), root)
	if err != nil {
		return err
	}
	embedder, ok := indexer.Embedder.(*EmbeddingClient)
	if !ok {
		return fmt.Errorf("embedding client is required to count tokens")
	}
	llm := NewLLMClient("http://localhost:11434", "qwen3:8b")
	llm.HTTPClient = &http.Client{Timeout: 120 * time.Second}
	assistant, err := indexed.Assistant(llm, embedder, 2048)
	if err != nil {
		return err
	}
	answer, err := assistant.Ask(question)
	if err != nil {
		return err
	}
	fmt.Println(answer)
	return nil
}

func runAgent(ctx context.Context, root, goal string) error {
	store, err := NewFilesystemIndexStore("")
	if err != nil {
		return err
	}
	config := DefaultIndexConfig()
	indexer := newCLIIndexer(store, config)
	indexed, err := indexer.Load(ctx, root)
	if err != nil {
		return err
	}

	tools := NewCodePilotTools(indexed)
	llm := NewLLMClient("http://localhost:11434", "qwen3:8b")
	llm.HTTPClient = &http.Client{Timeout: 120 * time.Second}
	agent := NewCodePilotAgent(tools, llm)

	fmt.Printf("Starting CodePilot Agent for goal: %q\n\n", goal)
	res, err := agent.Run(ctx, goal)
	if err != nil {
		return err
	}

	for _, step := range res.Steps {
		fmt.Printf("[Step %d]\n", step.StepNumber)
		if step.Thought != "" {
			fmt.Printf("Thought: %s\n", step.Thought)
		}
		if step.ToolName != "" {
			fmt.Printf("Action: %s(%s)\n", step.ToolName, step.ToolArgs)
		}
		if step.Observation != "" {
			fmt.Printf("Observation:\n%s\n\n", step.Observation)
		}
	}

	fmt.Printf("Final Answer:\n%s\n\n(Duration: %v, Completed: %t)\n", res.Answer, res.Duration.Round(time.Millisecond), res.Completed)
	return nil
}

func runTool(ctx context.Context, root, toolName, argsJSON string) error {
	_ = ctx
	store, err := NewFilesystemIndexStore("")
	if err != nil {
		return err
	}
	config := DefaultIndexConfig()
	indexer := newCLIIndexer(store, config)
	indexed, err := indexer.Load(context.Background(), root)
	if err != nil {
		return err
	}

	var args map[string]any
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Errorf("parse tool arguments JSON: %w", err)
		}
	}

	tools := NewCodePilotTools(indexed)
	result, err := tools.ExecuteTool(toolName, args)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func runEval(ctx context.Context, root string) error {
	_ = ctx
	store, err := NewFilesystemIndexStore("")
	if err != nil {
		return err
	}
	config := DefaultIndexConfig()
	indexer := newCLIIndexer(store, config)
	indexed, err := indexer.Load(context.Background(), root)
	if err != nil {
		return err
	}

	retriever := &HybridEvidenceRetriever{
		SemanticSearch: indexed.Engine,
		LexicalScorer:  &LexicalScorer{Tokenizer: CodeAwareTokenizer{}, Index: indexed.Lexical},
		Chunks:         indexed.Chunks,
		Graph:          indexed.Graph,
		Selector:       &EvidenceSelector{MaxChunks: 5, Policy: EvidencePolicyDirectFirst},
		CandidateLimit: 5,
		RRFK:           60,
	}

	dataset := StandardBenchmarkDataset()
	fmt.Printf("Running retrieval benchmark evaluation on %s (%d queries)...\n", root, len(dataset))

	report, err := EvaluateRetriever(retriever, dataset)
	if err != nil {
		return err
	}

	fmt.Println(report.String())

	gate := DefaultRegressionGate()
	if err := gate.Validate(report); err != nil {
		return fmt.Errorf("REGRESSION GATE FAILED:\n%w", err)
	}

	fmt.Println("SUCCESS: All regression gates passed.")
	return nil
}

func runMCP(ctx context.Context, root string) error {
	store, err := NewFilesystemIndexStore("")
	if err != nil {
		return err
	}
	config := DefaultIndexConfig()
	indexer := newCLIIndexer(store, config)
	indexed, err := indexer.Load(ctx, root)
	if err != nil {
		return err
	}

	tools := NewCodePilotTools(indexed)
	server := NewMCPServer(tools)

	fmt.Fprintf(os.Stderr, "CodePilot MCP server listening on stdio for %q...\n", root)
	return server.Serve(ctx, os.Stdin, os.Stdout)
}
