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
	scorer := &LexicalScorer{Tokenizer: CodeAwareTokenizer{}}
	const topK = 5

	cases := append(retrievalEvaluationCases(), RetrievalTestCase{Question: negativeRetrievalQuestion})
	var semanticRecallTotal, semanticMRRTotal float64
	var lexicalRecallTotal, lexicalMRRTotal float64
	for _, testCase := range cases {
		t.Run(testCase.Question, func(t *testing.T) {
			semantic, err := engine.Search(testCase.Question, len(engine.Documents))
			if err != nil {
				t.Fatalf("semantic Search() error = %v", err)
			}
			lexical, err := rankLexically(scorer, testCase.Question, chunks)
			if err != nil {
				t.Fatalf("rankLexically() error = %v", err)
			}

			t.Logf("Question: %s", testCase.Question)
			logTopResults(t, "Semantic", semantic, topK)
			topLexical := make([]CodeSearchResult, 0, topK)
			for i := 0; i < len(lexical) && i < topK; i++ {
				topLexical = append(topLexical, CodeSearchResult{Chunk: lexical[i].Chunk, Score: lexical[i].Score})
			}
			logTopResults(t, "Lexical", topLexical, topK)

			if len(testCase.ExpectedNames) > 0 {
				semanticRecall, semanticMRR := retrievalMetrics(semantic, testCase.ExpectedNames, topK)
				lexicalRecall, lexicalMRR := lexicalRetrievalMetrics(lexical, testCase.ExpectedNames, topK)
				t.Logf("Metrics: Semantic Recall@%d=%.3f MRR=%.3f; Lexical Recall@%d=%.3f MRR=%.3f",
					topK, semanticRecall, semanticMRR, topK, lexicalRecall, lexicalMRR)
				semanticRecallTotal += semanticRecall
				semanticMRRTotal += semanticMRR
				lexicalRecallTotal += lexicalRecall
				lexicalMRRTotal += lexicalMRR
			} else {
				t.Log("Negative example: no expected relevant chunks; recall and MRR are not calculated.")
			}

			logChangedRanks(t, semantic, lexical, topK)
		})
	}

	caseCount := float64(len(retrievalEvaluationCases()))
	t.Logf("Mean over %d answerable queries: Semantic Recall@%d=%.3f MRR=%.3f; Lexical Recall@%d=%.3f MRR=%.3f",
		int(caseCount), topK, semanticRecallTotal/caseCount, semanticMRRTotal/caseCount,
		topK, lexicalRecallTotal/caseCount, lexicalMRRTotal/caseCount)
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

func lexicalRetrievalMetrics(results []lexicalSearchResult, expectedNames []string, topK int) (float64, float64) {
	converted := make([]CodeSearchResult, len(results))
	for i, result := range results {
		converted[i] = CodeSearchResult{Chunk: result.Chunk, Score: result.Score}
	}
	return retrievalMetrics(converted, expectedNames, topK)
}

func logTopResults(t *testing.T, label string, results []CodeSearchResult, topK int) {
	t.Helper()
	t.Logf("%s top %d:", label, topK)
	for i := 0; i < len(results) && i < topK; i++ {
		result := results[i]
		t.Logf("%d. %.3f %s (%s)", i+1, result.Score, result.Chunk.Name, result.Chunk.SourceFile)
	}
}

func logChangedRanks(t *testing.T, semantic []CodeSearchResult, lexical []lexicalSearchResult, topK int) {
	t.Helper()
	semanticRanks := make(map[int]int, len(semantic))
	lexicalRanks := make(map[int]int, len(lexical))
	chunksByID := make(map[int]CodeChunk, len(semantic))
	for rank, result := range semantic {
		semanticRanks[result.Chunk.ID] = rank + 1
		chunksByID[result.Chunk.ID] = result.Chunk
	}
	for rank, result := range lexical {
		lexicalRanks[result.Chunk.ID] = rank + 1
		chunksByID[result.Chunk.ID] = result.Chunk
	}
	ids := make([]int, 0, len(chunksByID))
	for id := range chunksByID {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	t.Log("Changed ranks (chunks appearing in either top five):")
	for _, id := range ids {
		chunk := chunksByID[id]
		semanticRank, semanticInTop := semanticRanks[id]
		lexicalRank, lexicalInTop := lexicalRanks[id]
		semanticInTop = semanticInTop && semanticRank <= topK
		lexicalInTop = lexicalInTop && lexicalRank <= topK
		if !semanticInTop && !lexicalInTop {
			continue
		}
		if semanticRank == lexicalRank && semanticInTop && lexicalInTop {
			continue
		}
		semanticPosition := "not retrieved"
		if semanticRank, exists := semanticRanks[id]; exists {
			semanticPosition = fmt.Sprintf("#%d", semanticRank)
		}
		lexicalPosition := "not retrieved"
		if lexicalRank, exists := lexicalRanks[id]; exists {
			lexicalPosition = fmt.Sprintf("#%d", lexicalRank)
		}
		t.Logf("%s (%s): semantic %s, lexical %s", chunk.Name, chunk.SourceFile, semanticPosition, lexicalPosition)
	}
}
