package telemetry

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
			Chunk: CandidateChunk{Name: name},
			Score: 1.0 - float64(i)*0.1,
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

	if report.OverallRecallAt1 != 0.75 {
		t.Errorf("OverallRecallAt1 = %f, want 0.75", report.OverallRecallAt1)
	}
	if report.OverallRecallAt3 != 1.0 {
		t.Errorf("OverallRecallAt3 = %f, want 1.0", report.OverallRecallAt3)
	}
	if report.OverallMRR != 1.0 {
		t.Errorf("OverallMRR = %f, want 1.0", report.OverallMRR)
	}

	reportStr := report.String()
	if !strings.Contains(reportStr, "Retrieval Evaluation Report") {
		t.Errorf("report string missing title: %s", reportStr)
	}

	gate := DefaultRegressionGate()
	if err := gate.Validate(report); err != nil {
		t.Errorf("expected gate to pass, got: %v", err)
	}

	strictGate := RegressionGate{
		MinRecallAt1: 0.90,
	}
	if err := strictGate.Validate(report); err == nil {
		t.Error("expected strict gate to fail, got nil")
	}
}
