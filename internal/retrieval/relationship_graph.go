package retrieval

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"codepilot/internal/indexer"
)

const RelationshipCalls = "calls"
const RelationshipHasMethod = "has_method"

type CodeRelationship struct {
	FromChunkID int
	ToChunkID   int
	Kind        string
}

type CodeRelationshipGraph struct {
	Relationships []CodeRelationship
	chunksByID    map[int]CodeChunk
	bySource      map[int][]CodeRelationship
	byTarget      map[int][]CodeRelationship
}

type relationshipKey struct {
	from int
	to   int
	kind string
}

type relationshipPackageIndex struct {
	functions map[string][]CodeChunk
	methods   map[string][]CodeChunk
}

func BuildCodeRelationshipGraph(root string, chunks []CodeChunk) (*CodeRelationshipGraph, error) {
	graph := &CodeRelationshipGraph{
		chunksByID: make(map[int]CodeChunk, len(chunks)),
		bySource:   make(map[int][]CodeRelationship),
		byTarget:   make(map[int][]CodeRelationship),
	}
	chunksByFile := make(map[string]map[string]int)
	packages := make(map[string]*relationshipPackageIndex)
	files := make(map[string]struct{})
	for _, chunk := range chunks {
		graph.chunksByID[chunk.ID] = chunk
		sourceFile := filepath.ToSlash(filepath.Clean(filepath.FromSlash(chunk.SourceFile)))
		files[sourceFile] = struct{}{}
		if chunksByFile[sourceFile] == nil {
			chunksByFile[sourceFile] = make(map[string]int)
		}
		key := relationshipChunkKey(chunk.Name, chunk.ParentName)
		chunksByFile[sourceFile][key] = chunk.ID

		packageDir := filepath.ToSlash(filepath.Dir(sourceFile))
		if packages[packageDir] == nil {
			packages[packageDir] = &relationshipPackageIndex{
				functions: make(map[string][]CodeChunk),
				methods:   make(map[string][]CodeChunk),
			}
		}
		if chunk.Kind == ChunkKindFunction {
			packages[packageDir].functions[chunk.Name] = append(packages[packageDir].functions[chunk.Name], chunk)
		} else if chunk.Kind == ChunkKindMethod {
			packages[packageDir].methods[chunk.Name] = append(packages[packageDir].methods[chunk.Name], chunk)
		}
	}

	relationships := make(map[relationshipKey]struct{})
	for _, chunk := range chunks {
		if chunk.ParentID == 0 {
			continue
		}
		if _, parentExists := graph.chunksByID[chunk.ParentID]; !parentExists {
			continue
		}
		relationships[relationshipKey{from: chunk.ParentID, to: chunk.ID, kind: RelationshipHasMethod}] = struct{}{}
	}
	fileNames := make([]string, 0, len(files))
	for fileName := range files {
		fileNames = append(fileNames, fileName)
	}
	sort.Strings(fileNames)
	for _, sourceFile := range fileNames {
		path := filepath.Join(root, filepath.FromSlash(sourceFile))
		source, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read source file %q for relationship graph: %w", sourceFile, err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), sourceFile, source, 0)
		if err != nil {
			return nil, fmt.Errorf("parse source file %q for relationship graph: %w", sourceFile, err)
		}
		packageDir := filepath.ToSlash(filepath.Dir(sourceFile))
		packageIndex := packages[packageDir]
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			parentName := ""
			if function.Recv != nil {
				if len(function.Recv.List) != 1 {
					continue
				}
				parentName, err = indexer.ReceiverTypeName(function.Recv.List[0])
				if err != nil {
					return nil, fmt.Errorf("resolve receiver for method %q: %w", function.Name.Name, err)
				}
			}
			fromID, ok := chunksByFile[sourceFile][relationshipChunkKey(function.Name.Name, parentName)]
			if !ok {
				continue
			}

			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				toChunk, found := resolveCallTarget(call.Fun, packageIndex)
				if !found {
					return true
				}
				relationships[relationshipKey{from: fromID, to: toChunk.ID, kind: RelationshipCalls}] = struct{}{}
				return true
			})
		}
	}

	for key := range relationships {
		relationship := CodeRelationship{FromChunkID: key.from, ToChunkID: key.to, Kind: key.kind}
		graph.Relationships = append(graph.Relationships, relationship)
		graph.bySource[relationship.FromChunkID] = append(graph.bySource[relationship.FromChunkID], relationship)
		graph.byTarget[relationship.ToChunkID] = append(graph.byTarget[relationship.ToChunkID], relationship)
	}
	sort.Slice(graph.Relationships, func(i, j int) bool {
		if graph.Relationships[i].FromChunkID != graph.Relationships[j].FromChunkID {
			return graph.Relationships[i].FromChunkID < graph.Relationships[j].FromChunkID
		}
		if graph.Relationships[i].ToChunkID != graph.Relationships[j].ToChunkID {
			return graph.Relationships[i].ToChunkID < graph.Relationships[j].ToChunkID
		}
		return graph.Relationships[i].Kind < graph.Relationships[j].Kind
	})
	for sourceID := range graph.bySource {
		sort.Slice(graph.bySource[sourceID], func(i, j int) bool {
			if graph.bySource[sourceID][i].Kind != graph.bySource[sourceID][j].Kind {
				return graph.bySource[sourceID][i].Kind < graph.bySource[sourceID][j].Kind
			}
			return graph.bySource[sourceID][i].ToChunkID < graph.bySource[sourceID][j].ToChunkID
		})
	}
	return graph, nil
}

func (graph *CodeRelationshipGraph) ExpandCalls(candidates []CodeSearchResult) ([]EvidenceCandidate, error) {
	return graph.expandEvidence(candidates, RelationshipCalls)
}

func (graph *CodeRelationshipGraph) ExpandParents(candidates []CodeSearchResult) ([]EvidenceCandidate, error) {
	return graph.expandEvidence(candidates, RelationshipHasMethod)
}

func (graph *CodeRelationshipGraph) ExpandCallsAndParents(candidates []CodeSearchResult) ([]EvidenceCandidate, error) {
	return graph.expandEvidence(candidates, RelationshipCalls, RelationshipHasMethod)
}

// FindCallers returns all chunks that call targetChunkID.
func (graph *CodeRelationshipGraph) FindCallers(targetChunkID int) []CodeChunk {
	if graph == nil {
		return nil
	}
	var callers []CodeChunk
	seen := make(map[int]bool)
	for _, rel := range graph.byTarget[targetChunkID] {
		if rel.Kind != RelationshipCalls {
			continue
		}
		callerChunk, ok := graph.chunksByID[rel.FromChunkID]
		if !ok || seen[callerChunk.ID] {
			continue
		}
		seen[callerChunk.ID] = true
		callers = append(callers, callerChunk)
	}
	return callers
}

// FindCallees returns all chunks called by sourceChunkID.
func (graph *CodeRelationshipGraph) FindCallees(sourceChunkID int) []CodeChunk {
	if graph == nil {
		return nil
	}
	var callees []CodeChunk
	seen := make(map[int]bool)
	for _, rel := range graph.bySource[sourceChunkID] {
		if rel.Kind != RelationshipCalls {
			continue
		}
		calleeChunk, ok := graph.chunksByID[rel.ToChunkID]
		if !ok || seen[calleeChunk.ID] {
			continue
		}
		seen[calleeChunk.ID] = true
		callees = append(callees, calleeChunk)
	}
	return callees
}

func (graph *CodeRelationshipGraph) expandEvidence(candidates []CodeSearchResult, kinds ...string) ([]EvidenceCandidate, error) {
	if graph == nil {
		return nil, fmt.Errorf("code relationship graph must not be nil")
	}
	discovered := make([]EvidenceCandidate, 0, len(candidates))
	evidenceByID := make(map[int]int, len(candidates))
	appendEvidence := func(candidateEvidence EvidenceCandidate) {
		if index, ok := evidenceByID[candidateEvidence.Chunk.ID]; ok {
			// A chunk found directly is the stronger evidence source. Promote it
			// while retaining its ranked retrieval score.
			if candidateEvidence.Origin == EvidenceDirect {
				// Keep its position at the first discovery point for stable order.
				// Direct results themselves are already ranked by the retriever.
				// (The map only controls de-duplication.)
				discovered[index] = candidateEvidence
			}
			return
		}
		evidenceByID[candidateEvidence.Chunk.ID] = len(discovered)
		discovered = append(discovered, candidateEvidence)
	}
	for _, result := range candidates {
		candidate := result.Chunk
		appendEvidence(EvidenceCandidate{Chunk: candidate, Origin: EvidenceDirect, Score: result.Score})
		for _, relationship := range graph.byTarget[candidate.ID] {
			if relationship.Kind != RelationshipHasMethod || !includesString(kinds, RelationshipHasMethod) {
				continue
			}
			parent, ok := graph.chunksByID[relationship.FromChunkID]
			if ok {
				appendEvidence(EvidenceCandidate{Chunk: parent, Origin: EvidenceParent, AnchorID: candidate.ID})
			}
		}
		for _, relationship := range graph.bySource[candidate.ID] {
			if relationship.Kind != RelationshipCalls || !includesString(kinds, relationship.Kind) {
				continue
			}
			callee, ok := graph.chunksByID[relationship.ToChunkID]
			if ok {
				appendEvidence(EvidenceCandidate{Chunk: callee, Origin: EvidenceCallee, AnchorID: candidate.ID})
			}
		}
	}
	return discovered, nil
}

func includesString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func resolveCallTarget(function ast.Expr, packageIndex *relationshipPackageIndex) (CodeChunk, bool) {
	if packageIndex == nil {
		return CodeChunk{}, false
	}
	switch call := function.(type) {
	case *ast.Ident:
		return uniqueChunk(packageIndex.functions[call.Name])
	case *ast.SelectorExpr:
		// Selector calls are treated as method calls. If a method name is
		// ambiguous in this package, skip it instead of guessing a receiver.
		return uniqueChunk(packageIndex.methods[call.Sel.Name])
	default:
		return CodeChunk{}, false
	}
}

func uniqueChunk(chunks []CodeChunk) (CodeChunk, bool) {
	if len(chunks) != 1 {
		return CodeChunk{}, false
	}
	return chunks[0], true
}

func relationshipChunkKey(name, parentName string) string {
	return strings.Join([]string{parentName, name}, "\x00")
}
