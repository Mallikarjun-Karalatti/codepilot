package retrieval

import (
	"fmt"

	"codepilot/internal/indexer"
	"codepilot/internal/telemetry"
)

// IndexedRepository is a query-ready in-memory index plus its manifest.
type IndexedRepository struct {
	Root     string
	Manifest indexer.RepositoryManifest
	Engine   *CodeSearchEngine
	Chunks   []CodeChunk
	Graph    *CodeRelationshipGraph
	Lexical  *LexicalIndex
	Stats    indexer.IndexStats
	Metrics  telemetry.IndexMetrics
}

// NewIndexedRepository creates an IndexedRepository from an indexer.IndexState.
func NewIndexedRepository(state *indexer.IndexState, embedder Embedder) (*IndexedRepository, error) {
	if state == nil {
		return nil, fmt.Errorf("index state must not be nil")
	}

	chunks := make([]CodeChunk, len(state.Documents))
	for i, doc := range state.Documents {
		chunks[i] = doc.Chunk
	}

	tokenizer := CodeAwareTokenizer{}
	lexical, err := BuildLexicalIndex(chunks, tokenizer)
	if err != nil {
		return nil, fmt.Errorf("build lexical index: %w", err)
	}

	graph, err := BuildCodeRelationshipGraph(state.Root, chunks)
	if err != nil {
		return nil, fmt.Errorf("build code relationship graph: %w", err)
	}

	return &IndexedRepository{
		Root:     state.Root,
		Manifest: state.Manifest,
		Engine:   &CodeSearchEngine{Embedder: embedder, Documents: state.Documents},
		Chunks:   chunks,
		Graph:    graph,
		Lexical:  lexical,
		Stats:    state.Stats,
		Metrics:  state.Metrics,
	}, nil
}

// Retriever constructs a default HybridEvidenceRetriever for this repository.
func (repo *IndexedRepository) Retriever() *HybridEvidenceRetriever {
	return &HybridEvidenceRetriever{
		SemanticSearch: repo.Engine,
		LexicalScorer:  &LexicalScorer{Tokenizer: CodeAwareTokenizer{}, Index: repo.Lexical},
		Chunks:         repo.Chunks,
		Graph:          repo.Graph,
		Selector:       &EvidenceSelector{MaxChunks: 5, Policy: EvidencePolicyDirectFirst},
		CandidateLimit: 5,
		RRFK:           60,
	}
}
