package retrieval

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildCodeRelationshipGraphAndExpandCalls(t *testing.T) {
	root := t.TempDir()
	sources := map[string]string{
		"services.go": `package example

import "strings"

type UserService struct { DB *Database }

func (s *UserService) GetUser(id int) { _ = s.DB.FindUser(id) }
func (s *UserService) UpdateEmail(id int, email string) {
	s.DB.UpdateUserEmail(id, NormalizeToken(email))
}
func NormalizeToken(token string) string { return strings.TrimSpace(token) }
`,
		"database.go": `package example

type Database struct{}

func (db *Database) FindUser(id int) int { return id }
func (db *Database) UpdateUserEmail(id int, email string) {}
`,
	}
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
	}

	chunks := []CodeChunk{
		{ID: 1, SourceFile: "services.go", Kind: ChunkKindStruct, Name: "UserService"},
		{ID: 2, ParentID: 1, SourceFile: "services.go", Kind: ChunkKindMethod, Name: "GetUser", ParentName: "UserService"},
		{ID: 3, ParentID: 1, SourceFile: "services.go", Kind: ChunkKindMethod, Name: "UpdateEmail", ParentName: "UserService"},
		{ID: 4, SourceFile: "services.go", Kind: ChunkKindFunction, Name: "NormalizeToken"},
		{ID: 5, SourceFile: "database.go", Kind: ChunkKindStruct, Name: "Database"},
		{ID: 6, ParentID: 5, SourceFile: "database.go", Kind: ChunkKindMethod, Name: "FindUser", ParentName: "Database"},
		{ID: 7, ParentID: 5, SourceFile: "database.go", Kind: ChunkKindMethod, Name: "UpdateUserEmail", ParentName: "Database"},
	}
	graph, err := BuildCodeRelationshipGraph(root, chunks)
	if err != nil {
		t.Fatalf("BuildCodeRelationshipGraph() error = %v", err)
	}
	var callRelationships, parentRelationships []CodeRelationship
	for _, relationship := range graph.Relationships {
		switch relationship.Kind {
		case RelationshipCalls:
			callRelationships = append(callRelationships, relationship)
		case RelationshipHasMethod:
			parentRelationships = append(parentRelationships, relationship)
		}
	}
	wantCalls := []CodeRelationship{
		{FromChunkID: 2, ToChunkID: 6, Kind: RelationshipCalls},
		{FromChunkID: 3, ToChunkID: 4, Kind: RelationshipCalls},
		{FromChunkID: 3, ToChunkID: 7, Kind: RelationshipCalls},
	}
	if !reflect.DeepEqual(callRelationships, wantCalls) {
		t.Fatalf("call relationships = %#v, want %#v", callRelationships, wantCalls)
	}
	wantParentRelationships := []CodeRelationship{
		{FromChunkID: 1, ToChunkID: 2, Kind: RelationshipHasMethod},
		{FromChunkID: 1, ToChunkID: 3, Kind: RelationshipHasMethod},
		{FromChunkID: 5, ToChunkID: 6, Kind: RelationshipHasMethod},
		{FromChunkID: 5, ToChunkID: 7, Kind: RelationshipHasMethod},
	}
	if !reflect.DeepEqual(parentRelationships, wantParentRelationships) {
		t.Fatalf("parent relationships = %#v, want %#v", parentRelationships, wantParentRelationships)
	}

	direct := []CodeSearchResult{
		{Chunk: chunks[2], Score: 0.91},
		{Chunk: chunks[3], Score: 0.82},
	}
	selected, err := graph.ExpandCalls(direct)
	if err != nil {
		t.Fatalf("ExpandCalls() error = %v", err)
	}
	gotNames := make([]string, len(selected))
	for i, chunk := range selected {
		gotNames[i] = chunk.Chunk.Name
	}
	wantNames := []string{"UpdateEmail", "NormalizeToken", "UpdateUserEmail"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("ExpandCalls() names = %#v, want %#v", gotNames, wantNames)
	}
	wantEvidence := []EvidenceCandidate{
		{Chunk: chunks[2], Origin: EvidenceDirect, Score: 0.91},
		{Chunk: chunks[3], Origin: EvidenceDirect, Score: 0.82},
		{Chunk: chunks[6], Origin: EvidenceCallee, AnchorID: chunks[2].ID},
	}
	if !reflect.DeepEqual(selected, wantEvidence) {
		t.Fatalf("ExpandCalls() evidence = %#v, want %#v", selected, wantEvidence)
	}
}

func TestCodeRelationshipGraphExpandParents(t *testing.T) {
	root := t.TempDir()
	source := `package example

type UserService struct{}
func (s *UserService) GetUser() {}
func (s *UserService) UpdateEmail() {}

type AuthService struct{}
func (s *AuthService) AuthenticateUser() {}
func (s *AuthService) LogoutUser() {}

type Database struct{}
func (db *Database) FindUser() {}
func (db *Database) UpdateUserEmail() {}
func (db *Database) ValidateToken() {}
func (db *Database) RevokeSession() {}
func (db *Database) Close() {}
`
	if err := os.WriteFile(filepath.Join(root, "services.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	chunks := []CodeChunk{
		{ID: 1, SourceFile: "services.go", Kind: ChunkKindStruct, Name: "UserService"},
		{ID: 2, ParentID: 1, SourceFile: "services.go", Kind: ChunkKindMethod, Name: "GetUser", ParentName: "UserService"},
		{ID: 3, ParentID: 1, SourceFile: "services.go", Kind: ChunkKindMethod, Name: "UpdateEmail", ParentName: "UserService"},
		{ID: 4, SourceFile: "services.go", Kind: ChunkKindStruct, Name: "AuthService"},
		{ID: 5, ParentID: 4, SourceFile: "services.go", Kind: ChunkKindMethod, Name: "AuthenticateUser", ParentName: "AuthService"},
		{ID: 6, ParentID: 4, SourceFile: "services.go", Kind: ChunkKindMethod, Name: "LogoutUser", ParentName: "AuthService"},
		{ID: 7, SourceFile: "services.go", Kind: ChunkKindStruct, Name: "Database"},
		{ID: 8, ParentID: 7, SourceFile: "services.go", Kind: ChunkKindMethod, Name: "FindUser", ParentName: "Database"},
		{ID: 9, ParentID: 7, SourceFile: "services.go", Kind: ChunkKindMethod, Name: "UpdateUserEmail", ParentName: "Database"},
		{ID: 10, ParentID: 7, SourceFile: "services.go", Kind: ChunkKindMethod, Name: "ValidateToken", ParentName: "Database"},
		{ID: 11, ParentID: 7, SourceFile: "services.go", Kind: ChunkKindMethod, Name: "RevokeSession", ParentName: "Database"},
		{ID: 12, ParentID: 7, SourceFile: "services.go", Kind: ChunkKindMethod, Name: "Close", ParentName: "Database"},
	}
	graph, err := BuildCodeRelationshipGraph(root, chunks)
	if err != nil {
		t.Fatalf("BuildCodeRelationshipGraph() error = %v", err)
	}
	tests := []struct {
		parent CodeChunk
		child  CodeChunk
	}{
		{parent: chunks[0], child: chunks[1]},
		{parent: chunks[3], child: chunks[4]},
		{parent: chunks[6], child: chunks[7]},
	}
	for _, test := range tests {
		got, err := graph.ExpandParents([]CodeSearchResult{{Chunk: test.child, Score: 0.77}})
		if err != nil {
			t.Fatalf("ExpandParents(%s) error = %v", test.child.Name, err)
		}
		want := []EvidenceCandidate{
			{Chunk: test.child, Origin: EvidenceDirect, Score: 0.77},
			{Chunk: test.parent, Origin: EvidenceParent, AnchorID: test.child.ID},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ExpandParents(%s) = %#v, want %#v", test.child.Name, got, want)
		}
	}
	// A directly retrieved struct does not automatically pull in every method.
	got, err := graph.ExpandParents([]CodeSearchResult{{Chunk: chunks[0], Score: 0.9}})
	if err != nil {
		t.Fatalf("ExpandParents(struct) error = %v", err)
	}
	if want := []EvidenceCandidate{{Chunk: chunks[0], Origin: EvidenceDirect, Score: 0.9}}; !reflect.DeepEqual(got, want) {
		t.Errorf("ExpandParents(struct) = %#v, want %#v", got, want)
	}
}

func TestCodeRelationshipGraphExpandCallsValidation(t *testing.T) {
	var nilGraph *CodeRelationshipGraph
	if _, err := nilGraph.ExpandCalls(nil); err == nil {
		t.Fatal("ExpandCalls() error = nil, want nil graph error")
	}
}
