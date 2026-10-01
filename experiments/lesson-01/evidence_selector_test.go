package main

import (
	"reflect"
	"testing"
)

func TestEvidenceSelectorRespectsBudgetAndRequiredParents(t *testing.T) {
	parent := CodeChunk{ID: 1, Kind: ChunkKindStruct, Name: "AuthService"}
	method := CodeChunk{ID: 2, ParentID: parent.ID, Kind: ChunkKindMethod, Name: "AuthenticateUser"}
	secondDirect := CodeChunk{ID: 3, Kind: ChunkKindFunction, Name: "FindUser"}
	callee := CodeChunk{ID: 4, Kind: ChunkKindFunction, Name: "ValidateToken"}
	candidates := []EvidenceCandidate{
		{Chunk: method, Origin: EvidenceDirect, Score: 0.9},
		{Chunk: parent, Origin: EvidenceParent, AnchorID: method.ID},
		{Chunk: secondDirect, Origin: EvidenceDirect, Score: 0.8},
		{Chunk: callee, Origin: EvidenceCallee, AnchorID: method.ID},
	}

	got, err := (&EvidenceSelector{MaxChunks: 2}).Select(candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	want := []CodeChunk{method, parent}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Select() = %#v, want required method-parent evidence %#v", got, want)
	}

	got, err = (&EvidenceSelector{MaxChunks: 3}).Select(candidates)
	if err != nil {
		t.Fatalf("Select() with room for another direct hit error = %v", err)
	}
	want = []CodeChunk{method, parent, secondDirect}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Select() = %#v, want direct evidence before optional callee %#v", got, want)
	}

	got, err = (&EvidenceSelector{MaxChunks: 4}).Select(candidates)
	if err != nil {
		t.Fatalf("Select() with room for callee error = %v", err)
	}
	want = []CodeChunk{method, parent, secondDirect, callee}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Select() = %#v, want optional callee last %#v", got, want)
	}
}

func TestEvidenceSelectorSkipsMethodWhenRequiredParentCannotFit(t *testing.T) {
	method := CodeChunk{ID: 2, ParentID: 1, Kind: ChunkKindMethod, Name: "AuthenticateUser"}
	parent := CodeChunk{ID: 1, Kind: ChunkKindStruct, Name: "AuthService"}
	function := CodeChunk{ID: 3, Kind: ChunkKindFunction, Name: "FindUser"}
	candidates := []EvidenceCandidate{
		{Chunk: method, Origin: EvidenceDirect, Score: 0.9},
		{Chunk: parent, Origin: EvidenceParent, AnchorID: method.ID},
		{Chunk: function, Origin: EvidenceDirect, Score: 0.8},
	}

	got, err := (&EvidenceSelector{MaxChunks: 1}).Select(candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if want := []CodeChunk{function}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Select() = %#v, want method skipped and next direct candidate selected %#v", got, want)
	}
}

func TestEvidenceSelectorDeduplicatesDirectParentAndDependency(t *testing.T) {
	parent := CodeChunk{ID: 1, Kind: ChunkKindStruct, Name: "AuthService"}
	method := CodeChunk{ID: 2, ParentID: parent.ID, Kind: ChunkKindMethod, Name: "AuthenticateUser"}
	candidates := []EvidenceCandidate{
		{Chunk: parent, Origin: EvidenceDirect, Score: 0.9},
		{Chunk: method, Origin: EvidenceDirect, Score: 0.8},
		{Chunk: parent, Origin: EvidenceParent, AnchorID: method.ID},
	}

	got, err := (&EvidenceSelector{MaxChunks: 2}).Select(candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if want := []CodeChunk{parent, method}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Select() = %#v, want deduplicated parent and method %#v", got, want)
	}
}

func TestEvidenceSelectorRequiresParentEvidence(t *testing.T) {
	method := CodeChunk{ID: 2, ParentID: 1, Kind: ChunkKindMethod, Name: "AuthenticateUser"}
	_, err := (&EvidenceSelector{MaxChunks: 2}).Select([]EvidenceCandidate{{Chunk: method, Origin: EvidenceDirect}})
	if err == nil {
		t.Fatal("Select() error = nil, want missing required parent error")
	}
}

func TestEvidenceSelectorValidatesBudget(t *testing.T) {
	if _, err := (&EvidenceSelector{}).Select(nil); err == nil {
		t.Fatal("Select() error = nil, want invalid budget error")
	}
	var nilSelector *EvidenceSelector
	if _, err := nilSelector.Select(nil); err == nil {
		t.Fatal("Select() error = nil, want nil selector error")
	}
}

func TestEvidenceSelectorDirectFirstFillsDirectHitsBeforeParents(t *testing.T) {
	parent := CodeChunk{ID: 1, Kind: ChunkKindStruct, Name: "AuthService"}
	method := CodeChunk{ID: 2, ParentID: parent.ID, Kind: ChunkKindMethod, Name: "AuthenticateUser"}
	secondDirect := CodeChunk{ID: 3, Kind: ChunkKindFunction, Name: "FindUser"}
	callee := CodeChunk{ID: 4, Kind: ChunkKindFunction, Name: "ValidateToken"}
	candidates := []EvidenceCandidate{
		{Chunk: method, Origin: EvidenceDirect, Score: 0.9},
		{Chunk: parent, Origin: EvidenceParent, AnchorID: method.ID},
		{Chunk: callee, Origin: EvidenceCallee, AnchorID: method.ID},
		{Chunk: secondDirect, Origin: EvidenceDirect, Score: 0.8},
	}
	selector := &EvidenceSelector{MaxChunks: 2, Policy: EvidencePolicyDirectFirst}
	got, err := selector.Select(candidates)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if want := []CodeChunk{method, secondDirect}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Select() = %#v, want all fitting direct hits before structural evidence %#v", got, want)
	}

	selector.MaxChunks = 3
	got, err = selector.Select(candidates)
	if err != nil {
		t.Fatalf("Select() with one expansion slot error = %v", err)
	}
	if want := []CodeChunk{method, secondDirect, parent}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Select() = %#v, want parent before optional callee in leftover slot %#v", got, want)
	}
}
