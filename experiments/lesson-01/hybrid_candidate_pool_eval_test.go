package main

import "testing"

func TestRealHybridCandidatePoolEvaluation(t *testing.T) {
	engine := realRetrievalEngine(t)
	chunks := make([]CodeChunk, len(engine.Documents))
	for i, document := range engine.Documents {
		chunks[i] = document.Chunk
	}
	tokenizer := CodeAwareTokenizer{}
	lexicalIndex, err := BuildLexicalIndex(chunks, tokenizer)
	if err != nil {
		t.Fatalf("BuildLexicalIndex() error = %v", err)
	}
	lexicalScorer := &LexicalScorer{Tokenizer: tokenizer, Index: lexicalIndex}

	const (
		finalK = 5
		rrfK   = 60
	)
	candidatePools := []int{5, 10, 20}
	testCases := append(retrievalEvaluationCases(), RetrievalTestCase{Question: negativeRetrievalQuestion})
	totals := make(map[int]retrievalMetricTotals, len(candidatePools))
	for _, poolSize := range candidatePools {
		t.Logf("Candidate pool N=%d; final K=%d; RRF k=%d", poolSize, finalK, rrfK)
	}

	for _, testCase := range testCases {
		semanticRanked, err := engine.Search(testCase.Question, len(engine.Documents))
		if err != nil {
			t.Fatalf("semantic Search(%q) error = %v", testCase.Question, err)
		}
		lexicalRanked, err := rankLexically(lexicalScorer, testCase.Question, chunks)
		if err != nil {
			t.Fatalf("rankLexically(%q) error = %v", testCase.Question, err)
		}

		t.Logf("Question: %s", testCase.Question)
		for _, poolSize := range candidatePools {
			semanticCandidates := takeCodeSearchResults(semanticRanked, poolSize)
			lexicalCandidates := takeCodeSearchResults(asCodeSearchResults(lexicalRanked), poolSize)
			fused, err := ReciprocalRankFusion(semanticCandidates, lexicalCandidates, rrfK)
			if err != nil {
				t.Fatalf("ReciprocalRankFusion(N=%d) error = %v", poolSize, err)
			}
			hybridResults := hybridAsCodeSearchResults(fused)
			finalResults := takeCodeSearchResults(hybridResults, finalK)
			t.Logf("N=%d: semantic candidates=%d lexical candidates=%d fused candidates=%d",
				poolSize, len(semanticCandidates), len(lexicalCandidates), len(fused))
			logTopResults(t, "Hybrid final", finalResults, finalK)

			if len(testCase.ExpectedNames) == 0 {
				t.Logf("N=%d: negative example; no expected relevant chunks, so recall and MRR are not calculated", poolSize)
				continue
			}
			recall, mrr := retrievalMetrics(finalResults, testCase.ExpectedNames, finalK)
			t.Logf("N=%d: Recall@%d=%.3f (%d/%d), MRR=%.3f",
				poolSize, finalK, recall, int(recall*float64(len(testCase.ExpectedNames))), len(testCase.ExpectedNames), mrr)
			metricTotals := totals[poolSize]
			metricTotals.recall += recall
			metricTotals.mrr += mrr
			totals[poolSize] = metricTotals
		}
	}

	answerableCount := float64(len(retrievalEvaluationCases()))
	for _, poolSize := range candidatePools {
		metricTotals := totals[poolSize]
		t.Logf("Aggregate N=%d, K=%d: mean Recall@%d=%.3f, mean MRR=%.3f",
			poolSize, finalK, finalK, metricTotals.recall/answerableCount, metricTotals.mrr/answerableCount)
	}
}

type retrievalMetricTotals struct {
	recall float64
	mrr    float64
}

func takeCodeSearchResults(results []CodeSearchResult, limit int) []CodeSearchResult {
	if len(results) > limit {
		return results[:limit]
	}
	return results
}
