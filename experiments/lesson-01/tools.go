package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ToolParam describes a parameter in a tool's JSON schema.
type ToolParam struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

// ToolDefinition describes a deterministic repository intelligence tool.
type ToolDefinition struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Parameters  map[string]ToolParam `json:"parameters"`
	Handler     func(args map[string]any) (any, error)
}

// CodePilotTools exposes deterministic repository intelligence tools on top of an IndexedRepository.
type CodePilotTools struct {
	repo      *IndexedRepository
	retriever *HybridEvidenceRetriever
}

func NewCodePilotTools(repo *IndexedRepository) *CodePilotTools {
	retriever := &HybridEvidenceRetriever{
		SemanticSearch: repo.Engine,
		LexicalScorer:  &LexicalScorer{Tokenizer: CodeAwareTokenizer{}, Index: repo.Lexical},
		Chunks:         repo.Chunks,
		Graph:          repo.Graph,
		Selector:       &EvidenceSelector{MaxChunks: 5, Policy: EvidencePolicyDirectFirst},
		CandidateLimit: 5,
		RRFK:           60,
	}
	return &CodePilotTools{repo: repo, retriever: retriever}
}

// ToolSearchResult represents a search result formatted for tool consumers.
type ToolSearchResult struct {
	SourceFile string  `json:"source_file"`
	StartLine  int     `json:"start_line"`
	EndLine    int     `json:"end_line"`
	Kind       string  `json:"kind"`
	Name       string  `json:"name"`
	ParentName string  `json:"parent_name,omitempty"`
	Score      float64 `json:"score"`
	Text       string  `json:"text"`
}

// FileLineRange represents lines extracted from a file.
type FileLineRange struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Content   string `json:"content"`
}

// SymbolReference contains location and metadata of a symbol or chunk.
type SymbolReference struct {
	SourceFile string `json:"source_file"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	ParentName string `json:"parent_name,omitempty"`
}

// SearchCode performs hybrid (semantic + lexical + RRF) search for relevant code.
func (t *CodePilotTools) SearchCode(query string, limit int) ([]ToolSearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("search query must not be empty")
	}
	if limit <= 0 {
		limit = 5
	}
	candidates, err := t.retriever.Retrieve(query)
	if err != nil {
		return nil, fmt.Errorf("retrieve code: %w", err)
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	results := make([]ToolSearchResult, len(candidates))
	for i, c := range candidates {
		results[i] = ToolSearchResult{
			SourceFile: c.Chunk.SourceFile,
			StartLine:  c.Chunk.StartLine,
			EndLine:    c.Chunk.EndLine,
			Kind:       c.Chunk.Kind,
			Name:       c.Chunk.Name,
			ParentName: c.Chunk.ParentName,
			Score:      c.Score,
			Text:       c.Chunk.Text,
		}
	}
	return results, nil
}

// ReadFile reads lines from a repository file safely.
func (t *CodePilotTools) ReadFile(relPath string, startLine, endLine int) (*FileLineRange, error) {
	cleanRel := filepath.ToSlash(filepath.Clean(relPath))
	if strings.HasPrefix(cleanRel, "../") || cleanRel == ".." {
		return nil, fmt.Errorf("invalid path %q: path cannot escape repository root", relPath)
	}
	fullPath := filepath.Join(t.repo.Root, filepath.FromSlash(cleanRel))
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("open file %q: %w", cleanRel, err)
	}
	defer file.Close()

	if startLine <= 0 {
		startLine = 1
	}

	scanner := bufio.NewScanner(file)
	var lines []string
	currentLine := 0
	for scanner.Scan() {
		currentLine++
		if currentLine < startLine {
			continue
		}
		if endLine > 0 && currentLine > endLine {
			break
		}
		lines = append(lines, fmt.Sprintf("%4d | %s", currentLine, scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read file %q: %w", cleanRel, err)
	}

	actualEnd := currentLine
	if endLine > 0 && endLine < currentLine {
		actualEnd = endLine
	}
	return &FileLineRange{
		Path:      cleanRel,
		StartLine: startLine,
		EndLine:   actualEnd,
		Content:   strings.Join(lines, "\n"),
	}, nil
}

// ListFiles lists indexed files in the repository.
func (t *CodePilotTools) ListFiles(prefix string) ([]string, error) {
	var matches []string
	cleanPrefix := filepath.ToSlash(filepath.Clean(strings.TrimSpace(prefix)))
	if cleanPrefix == "." {
		cleanPrefix = ""
	}
	for path := range t.repo.Manifest.Files {
		if cleanPrefix == "" || strings.HasPrefix(path, cleanPrefix) {
			matches = append(matches, path)
		}
	}
	sort.Strings(matches)
	return matches, nil
}

// FindSymbol finds functions, methods, or structs matching the given symbol name.
func (t *CodePilotTools) FindSymbol(name string) ([]SymbolReference, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, fmt.Errorf("symbol name must not be empty")
	}
	var exact []SymbolReference
	var fuzzy []SymbolReference
	lower := strings.ToLower(trimmed)
	for _, chunk := range t.repo.Chunks {
		ref := SymbolReference{
			SourceFile: chunk.SourceFile,
			StartLine:  chunk.StartLine,
			EndLine:    chunk.EndLine,
			Kind:       chunk.Kind,
			Name:       chunk.Name,
			ParentName: chunk.ParentName,
		}
		if chunk.Name == trimmed {
			exact = append(exact, ref)
		} else if strings.EqualFold(chunk.Name, trimmed) || strings.Contains(strings.ToLower(chunk.Name), lower) {
			fuzzy = append(fuzzy, ref)
		}
	}
	if len(exact) > 0 {
		return exact, nil
	}
	return fuzzy, nil
}

// FindCallers finds functions or methods that call the specified function.
func (t *CodePilotTools) FindCallers(functionName string) ([]SymbolReference, error) {
	trimmed := strings.TrimSpace(functionName)
	if trimmed == "" {
		return nil, fmt.Errorf("function name must not be empty")
	}
	var targetChunkIDs []int
	for _, chunk := range t.repo.Chunks {
		if chunk.Name == trimmed && (chunk.Kind == ChunkKindFunction || chunk.Kind == ChunkKindMethod) {
			targetChunkIDs = append(targetChunkIDs, chunk.ID)
		}
	}
	if len(targetChunkIDs) == 0 {
		return nil, nil
	}

	seen := make(map[int]bool)
	var callers []SymbolReference
	for _, targetID := range targetChunkIDs {
		for _, rel := range t.repo.Graph.byTarget[targetID] {
			if rel.Kind != RelationshipCalls {
				continue
			}
			callerChunk, ok := t.repo.Graph.chunksByID[rel.FromChunkID]
			if !ok || seen[callerChunk.ID] {
				continue
			}
			seen[callerChunk.ID] = true
			callers = append(callers, SymbolReference{
				SourceFile: callerChunk.SourceFile,
				StartLine:  callerChunk.StartLine,
				EndLine:    callerChunk.EndLine,
				Kind:       callerChunk.Kind,
				Name:       callerChunk.Name,
				ParentName: callerChunk.ParentName,
			})
		}
	}
	return callers, nil
}

// FindCallees finds functions or methods called by the specified function.
func (t *CodePilotTools) FindCallees(functionName string) ([]SymbolReference, error) {
	trimmed := strings.TrimSpace(functionName)
	if trimmed == "" {
		return nil, fmt.Errorf("function name must not be empty")
	}
	var sourceChunkIDs []int
	for _, chunk := range t.repo.Chunks {
		if chunk.Name == trimmed && (chunk.Kind == ChunkKindFunction || chunk.Kind == ChunkKindMethod) {
			sourceChunkIDs = append(sourceChunkIDs, chunk.ID)
		}
	}
	if len(sourceChunkIDs) == 0 {
		return nil, nil
	}

	seen := make(map[int]bool)
	var callees []SymbolReference
	for _, sourceID := range sourceChunkIDs {
		for _, rel := range t.repo.Graph.bySource[sourceID] {
			if rel.Kind != RelationshipCalls {
				continue
			}
			calleeChunk, ok := t.repo.Graph.chunksByID[rel.ToChunkID]
			if !ok || seen[calleeChunk.ID] {
				continue
			}
			seen[calleeChunk.ID] = true
			callees = append(callees, SymbolReference{
				SourceFile: calleeChunk.SourceFile,
				StartLine:  calleeChunk.StartLine,
				EndLine:    calleeChunk.EndLine,
				Kind:       calleeChunk.Kind,
				Name:       calleeChunk.Name,
				ParentName: calleeChunk.ParentName,
			})
		}
	}
	return callees, nil
}

// GetFileOutline returns all structs, methods, and functions declared in a source file.
func (t *CodePilotTools) GetFileOutline(relPath string) ([]SymbolReference, error) {
	cleanRel := filepath.ToSlash(filepath.Clean(relPath))
	var symbols []SymbolReference
	for _, chunk := range t.repo.Chunks {
		if chunk.SourceFile == cleanRel {
			symbols = append(symbols, SymbolReference{
				SourceFile: chunk.SourceFile,
				StartLine:  chunk.StartLine,
				EndLine:    chunk.EndLine,
				Kind:       chunk.Kind,
				Name:       chunk.Name,
				ParentName: chunk.ParentName,
			})
		}
	}
	sort.Slice(symbols, func(i, j int) bool {
		return symbols[i].StartLine < symbols[j].StartLine
	})
	return symbols, nil
}

// Definitions returns the schema definitions for all tools supported by CodePilot.
func (t *CodePilotTools) Definitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "search_code",
			Description: "Search code semantically and lexically using hybrid retrieval and RRF ranking.",
			Parameters: map[string]ToolParam{
				"query": {Type: "string", Description: "Natural language query or code symbol to find.", Required: true},
				"limit": {Type: "integer", Description: "Maximum number of results to return (default 5)."},
			},
			Handler: func(args map[string]any) (any, error) {
				q, _ := args["query"].(string)
				limit := 5
				if l, ok := args["limit"].(float64); ok && l > 0 {
					limit = int(l)
				}
				return t.SearchCode(q, limit)
			},
		},
		{
			Name:        "read_file",
			Description: "Read lines from a file in the repository.",
			Parameters: map[string]ToolParam{
				"path":       {Type: "string", Description: "Relative path of the file to read.", Required: true},
				"start_line": {Type: "integer", Description: "1-indexed starting line number (default 1)."},
				"end_line":   {Type: "integer", Description: "1-indexed ending line number (default EOF)."},
			},
			Handler: func(args map[string]any) (any, error) {
				p, _ := args["path"].(string)
				start := 1
				end := 0
				if s, ok := args["start_line"].(float64); ok {
					start = int(s)
				}
				if e, ok := args["end_line"].(float64); ok {
					end = int(e)
				}
				return t.ReadFile(p, start, end)
			},
		},
		{
			Name:        "list_files",
			Description: "List files tracked in the repository index, optionally under a directory prefix.",
			Parameters: map[string]ToolParam{
				"prefix": {Type: "string", Description: "Optional directory prefix to filter files."},
			},
			Handler: func(args map[string]any) (any, error) {
				prefix, _ := args["prefix"].(string)
				return t.ListFiles(prefix)
			},
		},
		{
			Name:        "find_symbol",
			Description: "Find declarations of a function, method, or struct by name.",
			Parameters: map[string]ToolParam{
				"name": {Type: "string", Description: "Name of the symbol to find.", Required: true},
			},
			Handler: func(args map[string]any) (any, error) {
				name, _ := args["name"].(string)
				return t.FindSymbol(name)
			},
		},
		{
			Name:        "find_callers",
			Description: "Find functions and methods in the repository that call the specified function.",
			Parameters: map[string]ToolParam{
				"function_name": {Type: "string", Description: "Name of the callee function.", Required: true},
			},
			Handler: func(args map[string]any) (any, error) {
				fn, _ := args["function_name"].(string)
				return t.FindCallers(fn)
			},
		},
		{
			Name:        "find_callees",
			Description: "Find functions and methods in the repository called by the specified function.",
			Parameters: map[string]ToolParam{
				"function_name": {Type: "string", Description: "Name of the caller function.", Required: true},
			},
			Handler: func(args map[string]any) (any, error) {
				fn, _ := args["function_name"].(string)
				return t.FindCallees(fn)
			},
		},
		{
			Name:        "get_file_outline",
			Description: "Get the declared structs, functions, and methods in a source file.",
			Parameters: map[string]ToolParam{
				"path": {Type: "string", Description: "Relative path of the source file.", Required: true},
			},
			Handler: func(args map[string]any) (any, error) {
				p, _ := args["path"].(string)
				return t.GetFileOutline(p)
			},
		},
	}
}

// ExecuteTool finds a registered tool by name and executes it with provided arguments.
func (t *CodePilotTools) ExecuteTool(name string, args map[string]any) (string, error) {
	for _, def := range t.Definitions() {
		if def.Name == name {
			res, err := def.Handler(args)
			if err != nil {
				return "", err
			}
			bytes, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return fmt.Sprintf("%v", res), nil
			}
			return string(bytes), nil
		}
	}
	return "", fmt.Errorf("unknown tool %q", name)
}
