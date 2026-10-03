package main

import (
	"fmt"
	"strings"
)

// BenchmarkQuery represents an evaluation query with expected relevant symbols and category.
type BenchmarkQuery struct {
	Category      string   `json:"category"`
	Question      string   `json:"question"`
	ExpectedNames []string `json:"expected_names"`
}

// CategoryMetrics aggregates retrieval scores for a specific question archetype.
type CategoryMetrics struct {
	Category   string  `json:"category"`
	QueryCount int     `json:"query_count"`
	RecallAt1  float64 `json:"recall_at_1"`
	RecallAt3  float64 `json:"recall_at_3"`
	RecallAt5  float64 `json:"recall_at_5"`
	MRR        float64 `json:"mrr"`
}

// RetrievalEvaluationReport summarizes the full benchmark run and regression status.
type RetrievalEvaluationReport struct {
	TotalQueries     int                        `json:"total_queries"`
	PositiveQueries  int                        `json:"positive_queries"`
	NegativeQueries  int                        `json:"negative_queries"`
	OverallRecallAt1 float64                    `json:"overall_recall_at_1"`
	OverallRecallAt3 float64                    `json:"overall_recall_at_3"`
	OverallRecallAt5 float64                    `json:"overall_recall_at_5"`
	OverallMRR       float64                    `json:"overall_mrr"`
	NegativeScoreAvg float64                    `json:"negative_score_avg"`
	ByCategory       map[string]CategoryMetrics `json:"by_category"`
}

func (r RetrievalEvaluationReport) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Retrieval Evaluation Report (%d queries: %d positive, %d negative)\n",
		r.TotalQueries, r.PositiveQueries, r.NegativeQueries))
	sb.WriteString(fmt.Sprintf("  Overall: R@1=%.3f R@3=%.3f R@5=%.3f MRR=%.3f NegativeAvgScore=%.3f\n",
		r.OverallRecallAt1, r.OverallRecallAt3, r.OverallRecallAt5, r.OverallMRR, r.NegativeScoreAvg))
	sb.WriteString("  Category Breakdown:\n")
	for cat, m := range r.ByCategory {
		sb.WriteString(fmt.Sprintf("    %-20s (n=%2d): R@1=%.3f R@3=%.3f R@5=%.3f MRR=%.3f\n",
			cat, m.QueryCount, m.RecallAt1, m.RecallAt3, m.RecallAt5, m.MRR))
	}
	return sb.String()
}

// RegressionGate enforces minimum retrieval thresholds to prevent quality regressions.
type RegressionGate struct {
	MinRecallAt1 float64
	MinRecallAt3 float64
	MinRecallAt5 float64
	MinMRR       float64
}

func DefaultRegressionGate() RegressionGate {
	return RegressionGate{
		MinRecallAt1: 0.50,
		MinRecallAt3: 0.85,
		MinRecallAt5: 0.95,
		MinMRR:       0.88,
	}
}

func (g RegressionGate) Validate(report *RetrievalEvaluationReport) error {
	if report == nil {
		return fmt.Errorf("report is nil")
	}
	var violations []string
	if report.OverallRecallAt1 < g.MinRecallAt1 {
		violations = append(violations, fmt.Sprintf("Recall@1 %.3f < threshold %.3f", report.OverallRecallAt1, g.MinRecallAt1))
	}
	if report.OverallRecallAt3 < g.MinRecallAt3 {
		violations = append(violations, fmt.Sprintf("Recall@3 %.3f < threshold %.3f", report.OverallRecallAt3, g.MinRecallAt3))
	}
	if report.OverallRecallAt5 < g.MinRecallAt5 {
		violations = append(violations, fmt.Sprintf("Recall@5 %.3f < threshold %.3f", report.OverallRecallAt5, g.MinRecallAt5))
	}
	if report.OverallMRR < g.MinMRR {
		violations = append(violations, fmt.Sprintf("MRR %.3f < threshold %.3f", report.OverallMRR, g.MinMRR))
	}
	if len(violations) > 0 {
		return fmt.Errorf("retrieval regression gate failed:\n  %s", strings.Join(violations, "\n  "))
	}
	return nil
}

// EvidenceRetriever abstracts query evidence retrieval for evaluation.
type EvidenceRetriever interface {
	Retrieve(query string) ([]EvidenceCandidate, error)
}

// StandardBenchmarkDataset returns the canonical 30-query retrieval benchmark dataset.
func StandardBenchmarkDataset() []BenchmarkQuery {
	cases := retrievalBenchmarkCases()
	out := make([]BenchmarkQuery, len(cases))
	for i, c := range cases {
		out[i] = BenchmarkQuery{
			Category:      c.Category,
			Question:      c.Question,
			ExpectedNames: c.ExpectedNames,
		}
	}
	return out
}

// EvaluateRetriever runs the full benchmark suite against any EvidenceRetriever.
func EvaluateRetriever(retriever EvidenceRetriever, dataset []BenchmarkQuery) (*RetrievalEvaluationReport, error) {
	if retriever == nil {
		return nil, fmt.Errorf("retriever must not be nil")
	}
	if len(dataset) == 0 {
		return nil, fmt.Errorf("dataset must not be empty")
	}

	type rawQueryMetric struct {
		category string
		isPos    bool
		r1       float64
		r3       float64
		r5       float64
		mrr      float64
		topScore float64
	}

	var results []rawQueryMetric
	var totalNegScore float64
	var negCount int

	for _, q := range dataset {
		candidates, err := retriever.Retrieve(q.Question)
		if err != nil {
			return nil, fmt.Errorf("retrieve for question %q: %w", q.Question, err)
		}

		if len(q.ExpectedNames) == 0 {
			// Negative query
			negCount++
			topScore := 0.0
			if len(candidates) > 0 {
				topScore = candidates[0].Score
			}
			totalNegScore += topScore
			results = append(results, rawQueryMetric{
				category: q.Category,
				isPos:    false,
				topScore: topScore,
			})
			continue
		}

		// Positive query
		expectedSet := make(map[string]bool, len(q.ExpectedNames))
		for _, name := range q.ExpectedNames {
			expectedSet[name] = true
		}

		topNames := make([]string, len(candidates))
		for i, cand := range candidates {
			topNames[i] = cand.Chunk.Name
		}

		recallAtK := func(k int) float64 {
			limit := k
			if len(topNames) < limit {
				limit = len(topNames)
			}
			found := 0
			for exp := range expectedSet {
				for i := 0; i < limit; i++ {
					if topNames[i] == exp {
						found++
						break
					}
				}
			}
			return float64(found) / float64(len(expectedSet))
		}

		mrr := 0.0
		for i, name := range topNames {
			if expectedSet[name] {
				mrr = 1.0 / float64(i+1)
				break
			}
		}

		results = append(results, rawQueryMetric{
			category: q.Category,
			isPos:    true,
			r1:       recallAtK(1),
			r3:       recallAtK(3),
			r5:       recallAtK(5),
			mrr:      mrr,
		})
	}

	posCount := len(dataset) - negCount
	report := &RetrievalEvaluationReport{
		TotalQueries:    len(dataset),
		PositiveQueries: posCount,
		NegativeQueries: negCount,
		ByCategory:      make(map[string]CategoryMetrics),
	}

	if negCount > 0 {
		report.NegativeScoreAvg = totalNegScore / float64(negCount)
	}

	var sumR1, sumR3, sumR5, sumMRR float64
	byCatRaw := make(map[string][]rawQueryMetric)
	for _, r := range results {
		byCatRaw[r.category] = append(byCatRaw[r.category], r)
		if r.isPos {
			sumR1 += r.r1
			sumR3 += r.r3
			sumR5 += r.r5
			sumMRR += r.mrr
		}
	}

	if posCount > 0 {
		report.OverallRecallAt1 = sumR1 / float64(posCount)
		report.OverallRecallAt3 = sumR3 / float64(posCount)
		report.OverallRecallAt5 = sumR5 / float64(posCount)
		report.OverallMRR = sumMRR / float64(posCount)
	}

	for cat, catResults := range byCatRaw {
		catPos := 0
		var catR1, catR3, catR5, catMRR float64
		for _, cr := range catResults {
			if cr.isPos {
				catPos++
				catR1 += cr.r1
				catR3 += cr.r3
				catR5 += cr.r5
				catMRR += cr.mrr
			}
		}
		cm := CategoryMetrics{
			Category:   cat,
			QueryCount: len(catResults),
		}
		if catPos > 0 {
			cm.RecallAt1 = catR1 / float64(catPos)
			cm.RecallAt3 = catR3 / float64(catPos)
			cm.RecallAt5 = catR5 / float64(catPos)
			cm.MRR = catMRR / float64(catPos)
		}
		report.ByCategory[cat] = cm
	}

	return report, nil
}
