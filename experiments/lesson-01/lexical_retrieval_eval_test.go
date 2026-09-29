package main

import (
	"fmt"
	"sort"
	"testing"
)

type lexicalSearchResult struct {
	Chunk CodeChunk
	Score float64
}

func rankLexically(scorer *LexicalScorer, query string, chunks []CodeChunk) ([]lexicalSearchResult, error) {
	results := make([]lexicalSearchResult, 0, len(chunks))
	for _, chunk := range chunks {
		score, _, err := scorer.Score(query, chunk)
		if err != nil {
			return nil, err
		}
		if score == 0 {
			continue
		}
		results = append(results, lexicalSearchResult{Chunk: chunk, Score: score})
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results, nil
}

func TestRealLexicalRetrievalEvaluation(t *testing.T) {
	engine := realRetrievalEngine(t)
	chunks := make([]CodeChunk, len(engine.Documents))
	for i, document := range engine.Documents {
		chunks[i] = document.Chunk
	}
	tokenizer := CodeAwareTokenizer{}
	v1Scorer := &LexicalScorer{Tokenizer: tokenizer}
	lexicalIndex, err := BuildLexicalIndex(chunks, tokenizer)
	if err != nil {
		t.Fatalf("BuildLexicalIndex() error = %v", err)
	}
	v2Scorer := &LexicalScorer{Tokenizer: tokenizer, Index: lexicalIndex}
	const topK = 5

	cases := append(retrievalEvaluationCases(), RetrievalTestCase{Question: negativeRetrievalQuestion})
	var semanticRecallTotal, semanticMRRTotal float64
	var lexicalV1RecallTotal, lexicalV1MRRTotal float64
	var lexicalV2RecallTotal, lexicalV2MRRTotal float64
	for _, testCase := range cases {
		t.Run(testCase.Question, func(t *testing.T) {
			semantic, err := engine.Search(testCase.Question, len(engine.Documents))
			if err != nil {
				t.Fatalf("semantic Search() error = %v", err)
			}
			lexicalV1, err := rankLexically(v1Scorer, testCase.Question, chunks)
			if err != nil {
				t.Fatalf("rankLexically(v1) error = %v", err)
			}
			lexicalV2, err := rankLexically(v2Scorer, testCase.Question, chunks)
			if err != nil {
				t.Fatalf("rankLexically(v2) error = %v", err)
			}

			t.Logf("Question: %s", testCase.Question)
			logTopResults(t, "Semantic", semantic, topK)
			logTopResults(t, "Lexical-v1", asCodeSearchResults(lexicalV1), topK)
			logTopResults(t, "Lexical-v2 IDF", asCodeSearchResults(lexicalV2), topK)

			if len(testCase.ExpectedNames) > 0 {
				semanticRecall, semanticMRR := retrievalMetrics(semantic, testCase.ExpectedNames, topK)
				lexicalV1Recall, lexicalV1MRR := retrievalMetrics(asCodeSearchResults(lexicalV1), testCase.ExpectedNames, topK)
				lexicalV2Recall, lexicalV2MRR := retrievalMetrics(asCodeSearchResults(lexicalV2), testCase.ExpectedNames, topK)
				t.Logf("Metrics: Semantic Recall@%d=%.3f MRR=%.3f; Lexical-v1 Recall@%d=%.3f MRR=%.3f; Lexical-v2 Recall@%d=%.3f MRR=%.3f",
					topK, semanticRecall, semanticMRR,
					topK, lexicalV1Recall, lexicalV1MRR,
					topK, lexicalV2Recall, lexicalV2MRR)
				semanticRecallTotal += semanticRecall
				semanticMRRTotal += semanticMRR
				lexicalV1RecallTotal += lexicalV1Recall
				lexicalV1MRRTotal += lexicalV1MRR
				lexicalV2RecallTotal += lexicalV2Recall
				lexicalV2MRRTotal += lexicalV2MRR
			} else {
				t.Log("Negative example: no expected relevant chunks; recall and MRR are not calculated.")
			}

			t.Log("Rank changes: Semantic vs Lexical-v1")
			logRankChanges(t, "Semantic", semantic, "Lexical-v1", asCodeSearchResults(lexicalV1), topK)
			t.Log("Rank changes: Semantic vs Lexical-v2")
			logRankChanges(t, "Semantic", semantic, "Lexical-v2", asCodeSearchResults(lexicalV2), topK)
			t.Log("Rank changes: Lexical-v1 vs Lexical-v2")
			logRankChanges(t, "Lexical-v1", asCodeSearchResults(lexicalV1), "Lexical-v2", asCodeSearchResults(lexicalV2), topK)
		})
	}

	caseCount := float64(len(retrievalEvaluationCases()))
	t.Logf("Mean over %d answerable queries: Semantic Recall@%d=%.3f MRR=%.3f; Lexical-v1 Recall@%d=%.3f MRR=%.3f; Lexical-v2 Recall@%d=%.3f MRR=%.3f",
		int(caseCount),
		topK, semanticRecallTotal/caseCount, semanticMRRTotal/caseCount,
		topK, lexicalV1RecallTotal/caseCount, lexicalV1MRRTotal/caseCount,
		topK, lexicalV2RecallTotal/caseCount, lexicalV2MRRTotal/caseCount)
}

func retrievalMetrics(results []CodeSearchResult, expectedNames []string, topK int) (float64, float64) {
	found := make(map[string]bool, len(expectedNames))
	firstRank := 0
	for i := 0; i < len(results) && i < topK; i++ {
		for _, expected := range expectedNames {
			if results[i].Chunk.Name == expected {
				found[expected] = true
				if firstRank == 0 {
					firstRank = i + 1
				}
			}
		}
	}
	recall := float64(len(found)) / float64(len(expectedNames))
	mrr := 0.0
	if firstRank != 0 {
		mrr = 1 / float64(firstRank)
	}
	return recall, mrr
}

func asCodeSearchResults(results []lexicalSearchResult) []CodeSearchResult {
	converted := make([]CodeSearchResult, len(results))
	for i, result := range results {
		converted[i] = CodeSearchResult{Chunk: result.Chunk, Score: result.Score}
	}
	return converted
}

func logTopResults(t *testing.T, label string, results []CodeSearchResult, topK int) {
	t.Helper()
	t.Logf("%s top %d:", label, topK)
	for i := 0; i < len(results) && i < topK; i++ {
		result := results[i]
		t.Logf("%d. %.3f %s (%s)", i+1, result.Score, result.Chunk.Name, result.Chunk.SourceFile)
	}
}

func logRankChanges(t *testing.T, firstLabel string, first []CodeSearchResult, secondLabel string, second []CodeSearchResult, topK int) {
	t.Helper()
	firstRanks := make(map[int]int, len(first))
	secondRanks := make(map[int]int, len(second))
	chunksByID := make(map[int]CodeChunk, len(first))
	for rank, result := range first {
		firstRanks[result.Chunk.ID] = rank + 1
		chunksByID[result.Chunk.ID] = result.Chunk
	}
	for rank, result := range second {
		secondRanks[result.Chunk.ID] = rank + 1
		chunksByID[result.Chunk.ID] = result.Chunk
	}
	ids := make([]int, 0, len(chunksByID))
	for id := range chunksByID {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		chunk := chunksByID[id]
		firstRank, firstExists := firstRanks[id]
		secondRank, secondExists := secondRanks[id]
		firstInTop := firstExists && firstRank <= topK
		secondInTop := secondExists && secondRank <= topK
		if !firstInTop && !secondInTop {
			continue
		}
		if firstRank == secondRank && firstInTop && secondInTop {
			continue
		}
		firstPosition := "not retrieved"
		if firstExists {
			firstPosition = fmt.Sprintf("#%d", firstRank)
		}
		secondPosition := "not retrieved"
		if secondExists {
			secondPosition = fmt.Sprintf("#%d", secondRank)
		}
		t.Logf("%s (%s): %s %s, %s %s", chunk.Name, chunk.SourceFile, firstLabel, firstPosition, secondLabel, secondPosition)
	}
}
