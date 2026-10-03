package retrieval

import (
	"fmt"
	"sort"
)

type HybridSearchResult struct {
	Chunk         CodeChunk
	SemanticScore float64
	LexicalScore  float64
	CombinedScore float64
}

func ReciprocalRankFusion(
	semanticResults []CodeSearchResult,
	lexicalResults []CodeSearchResult,
	k int,
) ([]HybridSearchResult, error) {
	if k <= 0 {
		return nil, fmt.Errorf("RRF k must be greater than 0")
	}

	resultsByID := make(map[int]HybridSearchResult, len(semanticResults)+len(lexicalResults))
	order := make([]int, 0, len(semanticResults)+len(lexicalResults))
	addRankedResults := func(results []CodeSearchResult, semantic bool) {
		for i, result := range results {
			hybrid, exists := resultsByID[result.Chunk.ID]
			if !exists {
				hybrid.Chunk = result.Chunk
				order = append(order, result.Chunk.ID)
			}
			if semantic {
				hybrid.SemanticScore = result.Score
			} else {
				hybrid.LexicalScore = result.Score
			}
			hybrid.CombinedScore += 1 / float64(k+i+1)
			resultsByID[result.Chunk.ID] = hybrid
		}
	}

	addRankedResults(semanticResults, true)
	addRankedResults(lexicalResults, false)

	results := make([]HybridSearchResult, 0, len(order))
	for _, id := range order {
		results = append(results, resultsByID[id])
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].CombinedScore > results[j].CombinedScore
	})
	return results, nil
}
