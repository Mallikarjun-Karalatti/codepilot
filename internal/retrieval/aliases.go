package retrieval

import "codepilot/internal/indexer"

// Re-export indexer types for seamless usage in retrieval package.
type CodeChunk = indexer.CodeChunk
type CodeDocument = indexer.CodeDocument
type CodeSearchEngine = indexer.CodeSearchEngine
type CodeSearchResult = indexer.CodeSearchResult
type ChunkKind = indexer.ChunkKind
type Embedder = indexer.Embedder

const (
	ChunkKindFunction = indexer.ChunkKindFunction
	ChunkKindMethod   = indexer.ChunkKindMethod
	ChunkKindStruct   = indexer.ChunkKindStruct
)
