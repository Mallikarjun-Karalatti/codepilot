package retrieval

import (
	"math"
	"testing"
)

func TestReciprocalRankFusion(t *testing.T) {
	chunkA := CodeChunk{ID: 1, Name: "A"}
	chunkB := CodeChunk{ID: 2, Name: "B"}
	chunkC := CodeChunk{ID: 3, Name: "C"}
	semantic := []CodeSearchResult{
		{Chunk: chunkA, Score: 0.91},
		{Chunk: chunkB, Score: 0.82},
	}
	lexical := []CodeSearchResult{
		{Chunk: chunkB, Score: 12.4},
		{Chunk: chunkC, Score: 8.3},
	}

	got, err := ReciprocalRankFusion(semantic, lexical, 10)
	if err != nil {
		t.Fatalf("ReciprocalRankFusion() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ReciprocalRankFusion() returned %d results, want 3", len(got))
	}
	if got[0].Chunk.ID != chunkB.ID {
		t.Errorf("top result = %q, want B", got[0].Chunk.Name)
	}
	if got[0].SemanticScore != 0.82 || got[0].LexicalScore != 12.4 {
		t.Errorf("B scores = semantic %v lexical %v, want 0.82 and 12.4", got[0].SemanticScore, got[0].LexicalScore)
	}
	wantCombined := 1.0/12.0 + 1.0/11.0
	if math.Abs(got[0].CombinedScore-wantCombined) > 1e-12 {
		t.Errorf("B CombinedScore = %v, want %v", got[0].CombinedScore, wantCombined)
	}
	if got[1].Chunk.ID != chunkA.ID || got[1].SemanticScore != 0.91 || got[1].LexicalScore != 0 {
		t.Errorf("second result = %+v, want semantic-only A", got[1])
	}
	if got[2].Chunk.ID != chunkC.ID || got[2].SemanticScore != 0 || got[2].LexicalScore != 8.3 {
		t.Errorf("third result = %+v, want lexical-only C", got[2])
	}
}

func TestReciprocalRankFusionRequiresPositiveK(t *testing.T) {
	if _, err := ReciprocalRankFusion(nil, nil, 0); err == nil {
		t.Fatal("ReciprocalRankFusion() error = nil, want invalid k error")
	}
}

func TestReciprocalRankFusionEmptyLists(t *testing.T) {
	got, err := ReciprocalRankFusion(nil, nil, 60)
	if err != nil {
		t.Fatalf("ReciprocalRankFusion() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ReciprocalRankFusion() returned %d results, want 0", len(got))
	}
}
