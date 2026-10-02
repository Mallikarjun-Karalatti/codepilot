package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CodeIndexer struct {
	Chunker  *CodeChunker
	Embedder Embedder
}

type codeSourceFile struct {
	name   string
	dir    string
	source string
}

func (indexer *CodeIndexer) IndexRepository(root string) (*CodeSearchEngine, error) {
	if indexer == nil {
		return nil, fmt.Errorf("code indexer must not be nil")
	}
	if indexer.Embedder == nil {
		return nil, fmt.Errorf("embedder must not be nil")
	}

	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" && !strings.HasSuffix(path, "_test.go") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk repository %q: %w", root, err)
	}
	sort.Strings(paths)

	files := make([]codeSourceFile, 0, len(paths))
	structNamesByDir := make(map[string]map[string]struct{})
	for _, path := range paths {
		sourceBytes, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read source file %q: %w", path, err)
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return nil, fmt.Errorf("make source path relative for %q: %w", path, err)
		}
		relativePath = filepath.ToSlash(relativePath)
		dir := filepath.ToSlash(filepath.Dir(relativePath))
		source := string(sourceBytes)

		file, err := parser.ParseFile(token.NewFileSet(), relativePath, source, 0)
		if err != nil {
			return nil, fmt.Errorf("parse source file %q: %w", relativePath, err)
		}
		if structNamesByDir[dir] == nil {
			structNamesByDir[dir] = make(map[string]struct{})
		}
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
					structNamesByDir[dir][typeSpec.Name.Name] = struct{}{}
				}
			}
		}
		files = append(files, codeSourceFile{
			name:   relativePath,
			dir:    dir,
			source: source,
		})
	}

	chunker := indexer.Chunker
	if chunker == nil {
		chunker = &CodeChunker{}
	}
	chunks := make([]CodeChunk, 0)
	for _, file := range files {
		fileChunks, err := chunker.chunk(file.name, file.source, structNamesByDir[file.dir])
		if err != nil {
			return nil, fmt.Errorf("chunk source file %q: %w", file.name, err)
		}
		idOffset := len(chunks)
		for i := range fileChunks {
			fileChunks[i].ID += idOffset
			if fileChunks[i].ParentID != 0 {
				fileChunks[i].ParentID += idOffset
			}
		}
		chunks = append(chunks, fileChunks...)
	}

	if err := resolveCrossFileParentIDs(chunks); err != nil {
		return nil, err
	}

	engine := &CodeSearchEngine{Embedder: indexer.Embedder}
	if err := engine.Add(chunks); err != nil {
		return nil, fmt.Errorf("embed repository chunks: %w", err)
	}
	return engine, nil
}

func resolveCrossFileParentIDs(chunks []CodeChunk) error {
	structIDsByDir := make(map[string]map[string]int)
	for _, chunk := range chunks {
		if chunk.Kind != ChunkKindStruct {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(chunk.SourceFile))
		if structIDsByDir[dir] == nil {
			structIDsByDir[dir] = make(map[string]int)
		}
		structIDsByDir[dir][chunk.Name] = chunk.ID
	}
	for i := range chunks {
		if chunks[i].Kind != ChunkKindMethod {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(chunks[i].SourceFile))
		parentID, ok := structIDsByDir[dir][chunks[i].ParentName]
		if !ok {
			return fmt.Errorf(
				"struct %q not found in package directory %q for method %q",
				chunks[i].ParentName,
				dir,
				chunks[i].Name,
			)
		}
		chunks[i].ParentID = parentID
	}
	return nil
}
