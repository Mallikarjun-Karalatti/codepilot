package main

import (
	"os"
	"path/filepath"
	"testing"
)

type RetrievalTestCase struct {
	Question      string
	ExpectedNames []string
}

func retrievalEvaluationCases() []RetrievalTestCase {
	return []RetrievalTestCase{
		{
			Question:      "Where is authentication handled?",
			ExpectedNames: []string{"AuthenticateUser"},
		},
		{
			Question:      "How does authentication work?",
			ExpectedNames: []string{"AuthenticateUser", "ValidateToken"},
		},
		{
			Question:      "Where is a user's email updated?",
			ExpectedNames: []string{"UpdateEmail", "UpdateUserEmail"},
		},
		{
			Question:      "How does the application find a user?",
			ExpectedNames: []string{"GetUser", "FindUser"},
		},
	}
}

const negativeRetrievalQuestion = "Where are password reset emails sent?"

func realRetrievalEngine(t *testing.T) *CodeSearchEngine {
	t.Helper()
	if os.Getenv("CODEPILOT_REAL_OLLAMA") != "1" {
		t.Skip("set CODEPILOT_REAL_OLLAMA=1 to run the local Ollama retrieval evaluation")
	}

	embedder := NewEmbeddingClient("http://localhost:11434", "qwen3-embedding")
	engine, err := (&CodeIndexer{Embedder: embedder}).IndexRepository(filepath.Join("sample-project"))
	if err != nil {
		t.Fatalf("IndexRepository() error = %v", err)
	}
	return engine
}

func TestRealRetrievalEvaluation(t *testing.T) {
	engine := realRetrievalEngine(t)
	cases := retrievalEvaluationCases()

	const topK = 5
	var recallTotal, reciprocalRankTotal float64
	for _, testCase := range cases {
		t.Run(testCase.Question, func(t *testing.T) {
			results, err := engine.Search(testCase.Question, topK)
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}

			firstRelevantRank := 0
			relevantFound := make(map[string]int, len(testCase.ExpectedNames))
			t.Logf("Question: %s", testCase.Question)
			t.Logf("Top results (k=%d):", topK)
			for i, result := range results {
				rank := i + 1
				t.Logf("%d. %.6f %s (%s)", rank, result.Score, result.Chunk.Name, result.Chunk.SourceFile)
				for _, expected := range testCase.ExpectedNames {
					if result.Chunk.Name == expected {
						relevantFound[expected] = rank
						if firstRelevantRank == 0 {
							firstRelevantRank = rank
						}
					}
				}
			}

			recall := float64(len(relevantFound)) / float64(len(testCase.ExpectedNames))
			mrr := 0.0
			if firstRelevantRank > 0 {
				mrr = 1 / float64(firstRelevantRank)
			}
			t.Logf("Recall@%d: %.3f (%d/%d); MRR: %.3f", topK, recall, len(relevantFound), len(testCase.ExpectedNames), mrr)
			for _, expected := range testCase.ExpectedNames {
				if rank, ok := relevantFound[expected]; ok {
					t.Logf("expected chunk %q found at rank %d", expected, rank)
				} else {
					t.Errorf("expected chunk %q was not found in top %d", expected, topK)
				}
			}
			recallTotal += recall
			reciprocalRankTotal += mrr
		})
	}

	meanRecall := recallTotal / float64(len(cases))
	meanMRR := reciprocalRankTotal / float64(len(cases))
	t.Logf("Aggregate over %d questions: mean Recall@%d=%.3f; mean MRR=%.3f", len(cases), topK, meanRecall, meanMRR)

	t.Run("negative example: "+negativeRetrievalQuestion, func(t *testing.T) {
		results, err := engine.Search(negativeRetrievalQuestion, topK)
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		t.Logf("Question: %s", negativeRetrievalQuestion)
		t.Log("Negative example: no expected relevant chunks; Recall@5 and MRR are not calculated.")
		t.Logf("Top results (k=%d):", topK)
		for i, result := range results {
			t.Logf("%d. %.6f %s (%s)", i+1, result.Score, result.Chunk.Name, result.Chunk.SourceFile)
		}
	})
}

func TestRealRetrievalThresholdExperiment(t *testing.T) {
	engine := realRetrievalEngine(t)
	thresholds := []float64{0.15, 0.20, 0.25, 0.30, 0.35, 0.40}
	cases := append(retrievalEvaluationCases(), RetrievalTestCase{Question: negativeRetrievalQuestion})

	for _, testCase := range cases {
		results, err := engine.Search(testCase.Question, len(engine.Documents))
		if err != nil {
			t.Fatalf("Search(%q) error = %v", testCase.Question, err)
		}

		t.Logf("Question: %s", testCase.Question)
		for _, threshold := range thresholds {
			retrieved := 0
			foundExpected := make(map[string]bool, len(testCase.ExpectedNames))
			for _, result := range results {
				if result.Score < threshold {
					continue
				}
				retrieved++
				for _, expected := range testCase.ExpectedNames {
					if result.Chunk.Name == expected {
						foundExpected[expected] = true
					}
				}
			}

			if len(testCase.ExpectedNames) == 0 {
				t.Logf("threshold=%.2f retrieved=%d all expected retrieved=N/A (negative example)", threshold, retrieved)
				continue
			}
			allExpectedRetrieved := len(foundExpected) == len(testCase.ExpectedNames)
			t.Logf("threshold=%.2f retrieved=%d all expected retrieved=%t (%d/%d)",
				threshold, retrieved, allExpectedRetrieved, len(foundExpected), len(testCase.ExpectedNames))
		}
	}
}
