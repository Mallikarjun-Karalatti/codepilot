package main

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// IndexStats records work performed (and avoided) during an index update.
type IndexStats struct {
	FilesScanned   int
	FilesAdded     int
	FilesModified  int
	FilesDeleted   int
	FilesUnchanged int
	FilesParsed    int
	ChunksEmbedded int
	ChunksKept     int
	FullReindex    bool
}

// IndexedRepository is a query-ready in-memory index plus its manifest.
type IndexedRepository struct {
	Root     string
	Manifest RepositoryManifest
	Engine   *CodeSearchEngine
	Chunks   []CodeChunk
	Graph    *CodeRelationshipGraph
	Lexical  *LexicalIndex
	Stats    IndexStats
	Metrics  IndexMetrics
}

// IncrementalIndexer updates a persisted index using ChangeSet classification.
type IncrementalIndexer struct {
	Config   IndexConfig
	Chunker  *CodeChunker
	Embedder Embedder
	Scanner  *RepositoryScanner
	Hasher   FileHasher
	Store    IndexStore
	Logger   StructuredLogger
}

type parsedSourceFile struct {
	name        string
	dir         string
	source      string
	structNames []string
}

func (idx *IncrementalIndexer) Index(ctx context.Context, root string) (*IndexedRepository, error) {
	totalStart := time.Now()
	metrics := IndexMetrics{StartTime: totalStart}

	if idx == nil {
		return nil, fmt.Errorf("incremental indexer must not be nil")
	}
	if idx.Embedder == nil {
		return nil, fmt.Errorf("embedder must not be nil")
	}
	if idx.Store == nil {
		return nil, fmt.Errorf("index store must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cleanRoot := filepath.Clean(root)
	repoID, err := CanonicalRepositoryID(cleanRoot)
	if err != nil {
		return nil, err
	}
	fingerprint, err := idx.Config.Fingerprint()
	if err != nil {
		return nil, err
	}

	hasher := idx.Hasher
	if hasher == nil {
		hasher = &SHA256FileHasher{}
	}
	scanner := idx.Scanner
	if scanner == nil {
		scanner = &RepositoryScanner{Options: idx.Config.scannerOptions()}
	}
	chunker := idx.Chunker
	if chunker == nil {
		chunker = &CodeChunker{}
	}

	prev, err := idx.Store.Load(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("load persisted index: %w", err)
	}

	scanStart := time.Now()
	snapshots, err := scanner.Scan(ctx, cleanRoot)
	if err != nil {
		return nil, err
	}
	metrics.ScanDuration = time.Since(scanStart)

	var prevManifest *RepositoryManifest
	if prev != nil {
		prevManifest = &prev.Manifest
	}
	changeSet, err := (&ChangeDetector{Hasher: hasher, RootDir: cleanRoot}).Detect(
		ctx,
		prevManifest,
		repoID,
		fingerprint,
		snapshots,
	)
	if err != nil {
		return nil, err
	}

	stats := IndexStats{
		FilesScanned:   len(snapshots),
		FilesAdded:     len(changeSet.Added),
		FilesModified:  len(changeSet.Modified),
		FilesDeleted:   len(changeSet.Deleted),
		FilesUnchanged: len(changeSet.Unchanged),
		FullReindex:    changeSet.FullReindex,
	}

	unchanged := stringSet(changeSet.Unchanged)
	reprocess := stringSet(append(append([]string{}, changeSet.Added...), changeSet.Modified...))

	keptByFile := make(map[string][]CodeDocument)
	if prev != nil {
		docsByID := make(map[int]CodeDocument, len(prev.Documents))
		for _, doc := range prev.Documents {
			docsByID[doc.Chunk.ID] = doc
		}
		for path, meta := range prev.Manifest.Files {
			if !unchanged[path] {
				continue
			}
			for _, id := range meta.ChunkIDs {
				doc, ok := docsByID[id]
				if !ok {
					return nil, fmt.Errorf("manifest chunk id %d missing for unchanged file %q", id, path)
				}
				keptByFile[path] = append(keptByFile[path], doc)
				stats.ChunksKept++
			}
		}
	}

	structNamesByDir := structNamesFromDocuments(keptByFile)
	snapshotByPath := make(map[string]FileSnapshot, len(snapshots))

	var toProcess []FileSnapshot
	for _, snap := range snapshots {
		snapshotByPath[snap.Path] = snap
		if reprocess[snap.Path] {
			toProcess = append(toProcess, snap)
		}
	}
	sort.Slice(toProcess, func(i, j int) bool {
		return toProcess[i].Path < toProcess[j].Path
	})

	// 1. Concurrent AST Parsing with bounded worker pool
	parseStart := time.Now()
	parsedFiles := make([]parsedSourceFile, len(toProcess))
	if len(toProcess) > 0 {
		parseWorkers := idx.Config.EffectiveParseWorkers()
		if parseWorkers > len(toProcess) {
			parseWorkers = len(toProcess)
		}
		taskCh := make(chan int, len(toProcess))
		for i := range toProcess {
			taskCh <- i
		}
		close(taskCh)

		var parseErr error
		var parseOnce sync.Once
		var parseWg sync.WaitGroup

		for w := 0; w < parseWorkers; w++ {
			parseWg.Add(1)
			go func() {
				defer parseWg.Done()
				for i := range taskCh {
					if ctx.Err() != nil {
						parseOnce.Do(func() { parseErr = ctx.Err() })
						return
					}
					snap := toProcess[i]
					fullPath := filepath.Join(cleanRoot, filepath.FromSlash(snap.Path))
					sourceBytes, err := os.ReadFile(fullPath)
					if err != nil {
						if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
							continue
						}
						parseOnce.Do(func() { parseErr = fmt.Errorf("read %q: %w", snap.Path, err) })
						return
					}
					source := string(sourceBytes)
					file, err := parser.ParseFile(token.NewFileSet(), snap.Path, source, 0)
					if err != nil {
						parseOnce.Do(func() { parseErr = fmt.Errorf("parse %q: %w", snap.Path, err) })
						return
					}
					dir := filepath.ToSlash(filepath.Dir(snap.Path))
					var structs []string
					for _, decl := range file.Decls {
						genDecl, ok := decl.(*ast.GenDecl)
						if !ok || genDecl.Tok != token.TYPE {
							continue
						}
						for _, spec := range genDecl.Specs {
							typeSpec, ok := spec.(*ast.TypeSpec)
							if !ok {
								continue
							}
							if _, ok := typeSpec.Type.(*ast.StructType); ok {
								structs = append(structs, typeSpec.Name.Name)
							}
						}
					}
					parsedFiles[i] = parsedSourceFile{
						name:        snap.Path,
						dir:         dir,
						source:      source,
						structNames: structs,
					}
				}
			}()
		}
		parseWg.Wait()
		if parseErr != nil {
			return nil, parseErr
		}
	}
	metrics.ParseDuration = time.Since(parseStart)

	// Combine extracted struct names deterministically
	for _, p := range parsedFiles {
		if p.name == "" {
			continue
		}
		if structNamesByDir[p.dir] == nil {
			structNamesByDir[p.dir] = make(map[string]struct{})
		}
		for _, name := range p.structNames {
			structNamesByDir[p.dir][name] = struct{}{}
		}
		stats.FilesParsed++
	}

	// 2. Chunking
	chunkStart := time.Now()
	type fileChunksPair struct {
		path   string
		chunks []CodeChunk
	}
	var fileChunksList []fileChunksPair
	for _, p := range parsedFiles {
		if p.name == "" {
			continue
		}
		chunks, err := chunker.chunk(p.name, p.source, structNamesByDir[p.dir])
		if err != nil {
			return nil, fmt.Errorf("chunk %q: %w", p.name, err)
		}
		fileChunksList = append(fileChunksList, fileChunksPair{path: p.name, chunks: chunks})
	}
	metrics.ChunkDuration = time.Since(chunkStart)

	// 3. Concurrent Chunk Embedding with bounded worker pool
	embedStart := time.Now()
	type embedTask struct {
		path     string
		chunk    CodeChunk
		outIndex int
	}
	var tasks []embedTask
	for _, pair := range fileChunksList {
		for _, chunk := range pair.chunks {
			tasks = append(tasks, embedTask{
				path:     pair.path,
				chunk:    chunk,
				outIndex: len(tasks),
			})
		}
	}

	embeddedDocs := make([]CodeDocument, len(tasks))
	if len(tasks) > 0 {
		embedWorkers := idx.Config.EffectiveEmbedWorkers()
		if embedWorkers > len(tasks) {
			embedWorkers = len(tasks)
		}
		embedCh := make(chan int, len(tasks))
		for i := range tasks {
			embedCh <- i
		}
		close(embedCh)

		var embedErr error
		var embedOnce sync.Once
		var embedWg sync.WaitGroup

		for w := 0; w < embedWorkers; w++ {
			embedWg.Add(1)
			go func() {
				defer embedWg.Done()
				for i := range embedCh {
					if ctx.Err() != nil {
						embedOnce.Do(func() { embedErr = ctx.Err() })
						return
					}
					task := tasks[i]
					embedding, err := idx.Embedder.Embed(task.chunk.Text)
					if err != nil {
						embedOnce.Do(func() {
							embedErr = fmt.Errorf("embed chunk %s in %q: %w", task.chunk.Name, task.path, err)
						})
						return
					}
					if idx.Config.EmbeddingDimension > 0 && len(embedding) != idx.Config.EmbeddingDimension {
						embedOnce.Do(func() {
							embedErr = fmt.Errorf(
								"embedding dimension %d does not match configured %d",
								len(embedding),
								idx.Config.EmbeddingDimension,
							)
						})
						return
					}
					embeddedDocs[task.outIndex] = CodeDocument{Chunk: task.chunk, Embedding: embedding}
				}
			}()
		}
		embedWg.Wait()
		if embedErr != nil {
			return nil, embedErr
		}
		stats.ChunksEmbedded = len(tasks)
	}
	metrics.EmbedDuration = time.Since(embedStart)

	newByFile := make(map[string][]CodeDocument, len(fileChunksList))
	for _, doc := range embeddedDocs {
		newByFile[doc.Chunk.SourceFile] = append(newByFile[doc.Chunk.SourceFile], doc)
	}

	byFile := make(map[string][]CodeDocument)
	for path, docs := range keptByFile {
		byFile[path] = docs
	}
	for path, docs := range newByFile {
		byFile[path] = docs
	}

	documents, err := mergeDocumentsByFile(byFile)
	if err != nil {
		return nil, err
	}

	files := make(map[string]ManifestFile, len(byFile))
	chunkIDsByFile := make(map[string][]int)
	for _, doc := range documents {
		chunkIDsByFile[doc.Chunk.SourceFile] = append(chunkIDsByFile[doc.Chunk.SourceFile], doc.Chunk.ID)
	}

	for path := range byFile {
		snap, ok := snapshotByPath[path]
		if !ok {
			continue
		}
		meta := ManifestFile{
			Size:     snap.Size,
			ModTime:  snap.ModTime,
			ChunkIDs: chunkIDsByFile[path],
		}
		if unchanged[path] && prev != nil {
			if prevMeta, exists := prev.Manifest.Files[path]; exists {
				meta.SHA256 = prevMeta.SHA256
			}
		}
		if meta.SHA256 == "" {
			digest, err := hasher.HashFile(ctx, filepath.Join(cleanRoot, filepath.FromSlash(path)))
			if err != nil {
				if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("hash %q for manifest: %w", path, err)
			}
			meta.SHA256 = digest
		}
		files[path] = meta
	}

	manifest := RepositoryManifest{
		SchemaVersion:          indexSchemaVersion,
		RepositoryID:           repoID,
		IndexConfigFingerprint: fingerprint,
		GeneratedAt:            time.Now().UTC(),
		ExcludedDirectories:    idx.Config.normalizedExcluded(),
		IncludeTests:           idx.Config.IncludeTests,
		Files:                  files,
	}

	persistStart := time.Now()
	state := &PersistedIndex{Manifest: manifest, Documents: documents}
	if err := idx.Store.Commit(ctx, repoID, state); err != nil {
		return nil, fmt.Errorf("commit index: %w", err)
	}
	metrics.PersistDuration = time.Since(persistStart)
	metrics.TotalDuration = time.Since(totalStart)

	metrics.FilesScanned = stats.FilesScanned
	metrics.FilesAdded = stats.FilesAdded
	metrics.FilesModified = stats.FilesModified
	metrics.FilesDeleted = stats.FilesDeleted
	metrics.FilesUnchanged = stats.FilesUnchanged
	metrics.FilesParsed = stats.FilesParsed
	metrics.ChunksEmbedded = stats.ChunksEmbedded
	metrics.ChunksKept = stats.ChunksKept
	metrics.ChunksTotal = len(documents)
	metrics.FullReindex = stats.FullReindex

	if idx.Logger != nil {
		idx.Logger.Log("index_completed", map[string]any{
			"repository_id":   repoID,
			"files_scanned":   metrics.FilesScanned,
			"files_added":     metrics.FilesAdded,
			"files_modified":  metrics.FilesModified,
			"files_deleted":   metrics.FilesDeleted,
			"files_unchanged": metrics.FilesUnchanged,
			"chunks_embedded": metrics.ChunksEmbedded,
			"chunks_kept":     metrics.ChunksKept,
			"chunks_total":    metrics.ChunksTotal,
			"duration_ms":     metrics.TotalDuration.Milliseconds(),
		})
	}

	return idx.materialize(cleanRoot, manifest, documents, stats, metrics)
}

func (idx *IncrementalIndexer) Load(ctx context.Context, root string) (*IndexedRepository, error) {
	loadStart := time.Now()
	if idx == nil {
		return nil, fmt.Errorf("incremental indexer must not be nil")
	}
	if idx.Embedder == nil {
		return nil, fmt.Errorf("embedder must not be nil")
	}
	if idx.Store == nil {
		return nil, fmt.Errorf("index store must not be nil")
	}

	cleanRoot := filepath.Clean(root)
	repoID, err := CanonicalRepositoryID(cleanRoot)
	if err != nil {
		return nil, err
	}
	state, err := idx.Store.Load(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("load persisted index: %w", err)
	}
	if state == nil {
		return nil, fmt.Errorf("no persisted index for repository %q", cleanRoot)
	}
	stats := IndexStats{FilesUnchanged: len(state.Manifest.Files), ChunksKept: len(state.Documents)}
	metrics := IndexMetrics{
		StartTime:      loadStart,
		TotalDuration:  time.Since(loadStart),
		FilesUnchanged: len(state.Manifest.Files),
		ChunksKept:     len(state.Documents),
		ChunksTotal:    len(state.Documents),
	}
	return idx.materialize(cleanRoot, state.Manifest, state.Documents, stats, metrics)
}

func (idx *IncrementalIndexer) materialize(
	root string,
	manifest RepositoryManifest,
	documents []CodeDocument,
	stats IndexStats,
	metrics IndexMetrics,
) (*IndexedRepository, error) {
	chunks := make([]CodeChunk, len(documents))
	for i, doc := range documents {
		chunks[i] = doc.Chunk
	}
	tokenizer := CodeAwareTokenizer{}
	lexical, err := BuildLexicalIndex(chunks, tokenizer)
	if err != nil {
		return nil, err
	}
	graph, err := BuildCodeRelationshipGraph(root, chunks)
	if err != nil {
		return nil, err
	}
	return &IndexedRepository{
		Root:     root,
		Manifest: manifest,
		Engine:   &CodeSearchEngine{Embedder: idx.Embedder, Documents: documents},
		Chunks:   chunks,
		Graph:    graph,
		Lexical:  lexical,
		Stats:    stats,
		Metrics:  metrics,
	}, nil
}

func (indexed *IndexedRepository) Assistant(llm *LLMClient, tokenizer TokenCounter, maxTokens int) (*CodeAssistant, error) {
	if indexed == nil {
		return nil, fmt.Errorf("indexed repository must not be nil")
	}
	if llm == nil {
		return nil, fmt.Errorf("LLM client must not be nil")
	}
	if tokenizer == nil {
		return nil, fmt.Errorf("tokenizer must not be nil")
	}
	if maxTokens <= 0 {
		return nil, fmt.Errorf("max tokens must be greater than 0")
	}
	return &CodeAssistant{
		Retriever: &HybridEvidenceRetriever{
			SemanticSearch: indexed.Engine,
			LexicalScorer:  &LexicalScorer{Tokenizer: CodeAwareTokenizer{}, Index: indexed.Lexical},
			Chunks:         indexed.Chunks,
			Graph:          indexed.Graph,
			Selector:       &EvidenceSelector{MaxChunks: 5, Policy: EvidencePolicyDirectFirst},
			CandidateLimit: 5,
			RRFK:           60,
		},
		ContextBuilder: &ContextBuilder{
			Documents: indexed.Chunks,
			Tokenizer: tokenizer,
			MaxTokens: maxTokens,
		},
		Formatter: &ContextFormatter{},
		LLM:       llm,
	}, nil
}

func structNamesFromDocuments(byFile map[string][]CodeDocument) map[string]map[string]struct{} {
	names := make(map[string]map[string]struct{})
	for _, docs := range byFile {
		for _, doc := range docs {
			if doc.Chunk.Kind != ChunkKindStruct {
				continue
			}
			dir := filepath.ToSlash(filepath.Dir(doc.Chunk.SourceFile))
			if names[dir] == nil {
				names[dir] = make(map[string]struct{})
			}
			names[dir][doc.Chunk.Name] = struct{}{}
		}
	}
	return names
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func mergeDocumentsByFile(byFile map[string][]CodeDocument) ([]CodeDocument, error) {
	paths := make([]string, 0, len(byFile))
	for path := range byFile {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	combined := make([]CodeDocument, 0)
	for _, path := range paths {
		docs := normalizeFileDocuments(byFile[path])
		offset := len(combined)
		for i := range docs {
			docs[i].Chunk.SourceFile = path
			docs[i].Chunk.ID += offset
			if docs[i].Chunk.ParentID != 0 {
				docs[i].Chunk.ParentID += offset
			}
		}
		combined = append(combined, docs...)
	}

	chunks := make([]CodeChunk, len(combined))
	for i, doc := range combined {
		chunks[i] = doc.Chunk
	}
	if err := resolveCrossFileParentIDs(chunks); err != nil {
		return nil, err
	}
	for i := range combined {
		combined[i].Chunk = chunks[i]
	}
	return combined, nil
}

func normalizeFileDocuments(docs []CodeDocument) []CodeDocument {
	sorted := append([]CodeDocument(nil), docs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Chunk.StartLine != sorted[j].Chunk.StartLine {
			return sorted[i].Chunk.StartLine < sorted[j].Chunk.StartLine
		}
		if sorted[i].Chunk.Name != sorted[j].Chunk.Name {
			return sorted[i].Chunk.Name < sorted[j].Chunk.Name
		}
		return sorted[i].Chunk.Kind < sorted[j].Chunk.Kind
	})
	oldToNew := make(map[int]int, len(sorted))
	out := make([]CodeDocument, len(sorted))
	for i, doc := range sorted {
		newID := i + 1
		oldToNew[doc.Chunk.ID] = newID
		doc.Chunk.ID = newID
		out[i] = doc
	}
	for i := range out {
		parentID := out[i].Chunk.ParentID
		if parentID == 0 {
			continue
		}
		if remapped, ok := oldToNew[parentID]; ok {
			out[i].Chunk.ParentID = remapped
		} else {
			out[i].Chunk.ParentID = 0
		}
	}
	return out
}
