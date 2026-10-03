package main

import (
	"strings"
	"testing"
)

type mockRetriever struct {
	responses map[string][]string
}

func (m *mockRetriever) Retrieve(query string) ([]EvidenceCandidate, error) {
	names := m.responses[query]
	out := make([]EvidenceCandidate, len(names))
	for i, name := range names {
		out[i] = EvidenceCandidate{
			Chunk:  CodeChunk{ID: i + 1, Name: name},
			Origin: EvidenceDirect,
			Score:  1.0 - float64(i)*0.1,
		}
	}
	return out, nil
}

func TestEvaluatorRunnerAndRegressionGate(t *testing.T) {
	dataset := []BenchmarkQuery{
		{
			Category:      "exact",
			Question:      "Where is Authenticate?",
			ExpectedNames: []string{"AuthenticateUser"},
		},
		{
			Category:      "conceptual",
			Question:      "How to find user?",
			ExpectedNames: []string{"GetUser", "FindUser"},
		},
		{
			Category: "negative",
			Question: "How to launch rocket?",
		},
	}

	retriever := &mockRetriever{
		responses: map[string][]string{
			"Where is Authenticate?": {"AuthenticateUser", "OtherFunc"},
			"How to find user?":      {"GetUser", "FindUser"},
			"How to launch rocket?":  {"IrrelevantFunc"},
		},
	}

	report, err := EvaluateRetriever(retriever, dataset)
	if err != nil {
		t.Fatalf("EvaluateRetriever() error = %v", err)
	}

	if report.TotalQueries != 3 {
		t.Errorf("TotalQueries = %d, want 3", report.TotalQueries)
	}
	if report.PositiveQueries != 2 {
		t.Errorf("PositiveQueries = %d, want 2", report.PositiveQueries)
	}
	if report.NegativeQueries != 1 {
		t.Errorf("NegativeQueries = %d, want 1", report.NegativeQueries)
	}
	if report.OverallRecallAt1 != 0.75 { // query 1 has 1/1, query 2 has 1/2 = 0.5; avg = (1.0 + 0.5)/2 = 0.75
		t.Errorf("OverallRecallAt1 = %.3f, want 0.750", report.OverallRecallAt1)
	}
	if report.OverallRecallAt3 != 1.0 { // both have 100% recall at 3
		t.Errorf("OverallRecallAt3 = %.3f, want 1.000", report.OverallRecallAt3)
	}
	if report.OverallMRR != 1.0 { // both had first target at rank 1
		t.Errorf("OverallMRR = %.3f, want 1.000", report.OverallMRR)
	}

	// Test Regression Gate Pass
	gate := DefaultRegressionGate()
	if err := gate.Validate(report); err != nil {
		t.Errorf("expected gate to pass, got: %v", err)
	}

	// Test Regression Gate Failure on strict requirement
	strictGate := RegressionGate{MinRecallAt1: 0.90}
	err = strictGate.Validate(report)
	if err == nil {
		t.Fatal("expected strict gate to fail, got nil")
	}
	if !strings.Contains(err.Error(), "Recall@1") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestStandardBenchmarkDatasetIntegrity(t *testing.T) {
	dataset := StandardBenchmarkDataset()
	if len(dataset) != 30 {
		t.Fatalf("expected 30 benchmark queries, got %d", len(dataset))
	}
	negCount := 0
	for _, q := range dataset {
		if q.Category == "negative" {
			negCount++
		}
	}
	if negCount != 5 {
		t.Errorf("expected 5 negative benchmark queries, got %d", negCount)
	}
}
