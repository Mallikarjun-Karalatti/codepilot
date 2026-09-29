package main

import "testing"

func TestRealHybridRetrievalEvaluation(t *testing.T) {
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
		topK = 5
		rrfK = 60
	)
	cases := append(retrievalEvaluationCases(), RetrievalTestCase{Question: negativeRetrievalQuestion})
	var semanticRecallTotal, semanticMRRTotal float64
	var lexicalRecallTotal, lexicalMRRTotal float64
	var hybridRecallTotal, hybridMRRTotal float64
	for _, testCase := range cases {
		t.Run(testCase.Question, func(t *testing.T) {
			semantic, err := engine.Search(testCase.Question, topK)
			if err != nil {
				t.Fatalf("semantic Search() error = %v", err)
			}
			lexical, err := rankLexically(lexicalScorer, testCase.Question, chunks)
			if err != nil {
				t.Fatalf("rankLexically() error = %v", err)
			}
			if len(lexical) > topK {
				lexical = lexical[:topK]
			}
			lexicalResults := asCodeSearchResults(lexical)
			hybrid, err := ReciprocalRankFusion(semantic, lexicalResults, rrfK)
			if err != nil {
				t.Fatalf("ReciprocalRankFusion() error = %v", err)
			}

			t.Logf("Question: %s", testCase.Question)
			logTopResults(t, "Semantic", semantic, topK)
			logTopResults(t, "Lexical-v2 IDF", lexicalResults, topK)
			logHybridTopResults(t, hybrid, topK)

			hybridResults := hybridAsCodeSearchResults(hybrid)
			if len(testCase.ExpectedNames) > 0 {
				semanticRecall, semanticMRR := retrievalMetrics(semantic, testCase.ExpectedNames, topK)
				lexicalRecall, lexicalMRR := retrievalMetrics(lexicalResults, testCase.ExpectedNames, topK)
				hybridRecall, hybridMRR := retrievalMetrics(hybridResults, testCase.ExpectedNames, topK)
				t.Logf("Recall@%d / MRR: Semantic %.3f / %.3f; Lexical-v2 %.3f / %.3f; Hybrid %.3f / %.3f",
					topK, semanticRecall, semanticMRR, lexicalRecall, lexicalMRR, hybridRecall, hybridMRR)
				semanticRecallTotal += semanticRecall
				semanticMRRTotal += semanticMRR
				lexicalRecallTotal += lexicalRecall
				lexicalMRRTotal += lexicalMRR
				hybridRecallTotal += hybridRecall
				hybridMRRTotal += hybridMRR
			} else {
				t.Log("Negative example: no expected relevant chunks; recall and MRR are not calculated.")
			}

			t.Log("Rank changes: Semantic vs Hybrid")
			logRankChanges(t, "Semantic", semantic, "Hybrid", hybridResults, topK)
			t.Log("Rank changes: Lexical-v2 vs Hybrid")
			logRankChanges(t, "Lexical-v2", lexicalResults, "Hybrid", hybridResults, topK)
		})
	}

	answerableCount := float64(len(retrievalEvaluationCases()))
	t.Logf("Mean over %d answerable queries: Semantic Recall@%d=%.3f MRR=%.3f; Lexical-v2 Recall@%d=%.3f MRR=%.3f; Hybrid Recall@%d=%.3f MRR=%.3f",
		int(answerableCount),
		topK, semanticRecallTotal/answerableCount, semanticMRRTotal/answerableCount,
		topK, lexicalRecallTotal/answerableCount, lexicalMRRTotal/answerableCount,
		topK, hybridRecallTotal/answerableCount, hybridMRRTotal/answerableCount)
}

func hybridAsCodeSearchResults(results []HybridSearchResult) []CodeSearchResult {
	converted := make([]CodeSearchResult, len(results))
	for i, result := range results {
		converted[i] = CodeSearchResult{Chunk: result.Chunk, Score: result.CombinedScore}
	}
	return converted
}

func logHybridTopResults(t *testing.T, results []HybridSearchResult, topK int) {
	t.Helper()
	t.Logf("Hybrid RRF top %d:", topK)
	for i := 0; i < len(results) && i < topK; i++ {
		result := results[i]
		t.Logf("%d. combined=%.6f semantic=%.6f lexical=%.6f %s (%s)",
			i+1, result.CombinedScore, result.SemanticScore, result.LexicalScore,
			result.Chunk.Name, result.Chunk.SourceFile)
	}
}
