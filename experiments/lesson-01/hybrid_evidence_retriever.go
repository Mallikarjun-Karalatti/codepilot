package main

import (
	"fmt"
	"sort"
)

// HybridEvidenceRetriever owns the measured retrieval sequence: semantic and
// lexical top-K, RRF fusion, graph expansion, then Direct-First selection.
type HybridEvidenceRetriever struct {
	SemanticSearch *CodeSearchEngine
	LexicalScorer  *LexicalScorer
	Chunks         []CodeChunk
	Graph          *CodeRelationshipGraph
	Selector       *EvidenceSelector
	CandidateLimit int
	RRFK           int
}

func (retriever *HybridEvidenceRetriever) Retrieve(question string) ([]EvidenceCandidate, error) {
	if retriever == nil {
		return nil, fmt.Errorf("hybrid evidence retriever must not be nil")
	}
	if retriever.SemanticSearch == nil {
		return nil, fmt.Errorf("semantic search engine must not be nil")
	}
	if retriever.LexicalScorer == nil {
		return nil, fmt.Errorf("lexical scorer must not be nil")
	}
	if retriever.Selector == nil {
		return nil, fmt.Errorf("evidence selector must not be nil")
	}
	if retriever.CandidateLimit <= 0 {
		return nil, fmt.Errorf("candidate limit must be greater than 0")
	}
	rrfK := retriever.RRFK
	if rrfK == 0 {
		rrfK = 60
	}
	if rrfK < 0 {
		return nil, fmt.Errorf("RRF k must not be negative")
	}

	semantic, err := retriever.SemanticSearch.Search(question, retriever.CandidateLimit)
	if err != nil {
		return nil, fmt.Errorf("semantic retrieval: %w", err)
	}
	chunks := retriever.Chunks
	if chunks == nil {
		chunks = make([]CodeChunk, 0, len(retriever.SemanticSearch.Documents))
		for _, document := range retriever.SemanticSearch.Documents {
			chunks = append(chunks, document.Chunk)
		}
	}
	lexical := make([]CodeSearchResult, 0, len(chunks))
	for _, chunk := range chunks {
		score, _, err := retriever.LexicalScorer.Score(question, chunk)
		if err != nil {
			return nil, fmt.Errorf("score chunk %d lexically: %w", chunk.ID, err)
		}
		lexical = append(lexical, CodeSearchResult{Chunk: chunk, Score: score})
	}
	sort.SliceStable(lexical, func(i, j int) bool { return lexical[i].Score > lexical[j].Score })
	if len(lexical) > retriever.CandidateLimit {
		lexical = lexical[:retriever.CandidateLimit]
	}
	hybrid, err := ReciprocalRankFusion(semantic, lexical, rrfK)
	if err != nil {
		return nil, fmt.Errorf("fuse retrieval rankings: %w", err)
	}
	if len(hybrid) > retriever.CandidateLimit {
		hybrid = hybrid[:retriever.CandidateLimit]
	}
	direct := make([]CodeSearchResult, len(hybrid))
	for i, result := range hybrid {
		direct[i] = CodeSearchResult{Chunk: result.Chunk, Score: result.CombinedScore}
	}

	return retriever.SelectHybrid(direct)
}

// SelectHybrid expands and selects a previously fused candidate list. Keeping
// this stage separate lets evaluations compare policies on identical RRF input.
func (retriever *HybridEvidenceRetriever) SelectHybrid(direct []CodeSearchResult) ([]EvidenceCandidate, error) {
	if retriever == nil {
		return nil, fmt.Errorf("hybrid evidence retriever must not be nil")
	}
	if retriever.Selector == nil {
		return nil, fmt.Errorf("evidence selector must not be nil")
	}
	var evidence []EvidenceCandidate
	var err error
	if retriever.Graph == nil {
		evidence = make([]EvidenceCandidate, len(direct))
		for i, result := range direct {
			evidence[i] = EvidenceCandidate{Chunk: result.Chunk, Origin: EvidenceDirect, Score: result.Score}
		}
	} else {
		evidence, err = retriever.Graph.ExpandCallsAndParents(direct)
		if err != nil {
			return nil, fmt.Errorf("expand structural evidence: %w", err)
		}
	}
	selected, err := retriever.Selector.SelectEvidence(evidence)
	if err != nil {
		return nil, fmt.Errorf("select evidence: %w", err)
	}
	return selected, nil
}
