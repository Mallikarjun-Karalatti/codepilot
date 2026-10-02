package main

import (
	"context"
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
	if len(args) == 0 {
		return fmt.Errorf("usage: codepilot <index|ask> <repository> [question...]")
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
	default:
		return fmt.Errorf("unknown command %q\nusage: codepilot <index|ask> <repository> [question...]", args[0])
	}
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
	fmt.Printf(
		"indexed %s\n  files scanned=%d added=%d modified=%d deleted=%d unchanged=%d\n  parsed=%d chunks embedded=%d kept=%d full_reindex=%t\n  chunks total=%d\n",
		indexed.Root,
		indexed.Stats.FilesScanned,
		indexed.Stats.FilesAdded,
		indexed.Stats.FilesModified,
		indexed.Stats.FilesDeleted,
		indexed.Stats.FilesUnchanged,
		indexed.Stats.FilesParsed,
		indexed.Stats.ChunksEmbedded,
		indexed.Stats.ChunksKept,
		indexed.Stats.FullReindex,
		len(indexed.Chunks),
	)
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
