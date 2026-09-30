package main

import (
	"fmt"
	"sort"
	"testing"
)

type RetrievalBenchmarkCase struct {
	Category      string
	Question      string
	ExpectedNames []string
}

func retrievalBenchmarkCases() []RetrievalBenchmarkCase {
	return []RetrievalBenchmarkCase{
		{Category: "exact identifier", Question: "Where is AuthenticateUser defined?", ExpectedNames: []string{"AuthenticateUser"}},
		{Category: "exact identifier", Question: "FindUser", ExpectedNames: []string{"FindUser"}},
		{Category: "exact identifier", Question: "UpdateUserEmail", ExpectedNames: []string{"UpdateUserEmail"}},
		{Category: "exact identifier", Question: "What does NormalizeToken do?", ExpectedNames: []string{"NormalizeToken"}},
		{Category: "natural language", Question: "Where is authentication handled?", ExpectedNames: []string{"AuthenticateUser"}},
		{Category: "natural language", Question: "How does authentication work?", ExpectedNames: []string{"AuthenticateUser", "ValidateToken"}},
		{Category: "natural language", Question: "Where is a user's email updated?", ExpectedNames: []string{"UpdateEmail", "UpdateUserEmail"}},
		{Category: "natural language", Question: "How does the application find a user?", ExpectedNames: []string{"GetUser", "FindUser"}},
		{Category: "natural language", Question: "How is whitespace removed from a token?", ExpectedNames: []string{"NormalizeToken"}},
		{Category: "conceptual", Question: "What prevents revoked tokens from authenticating?", ExpectedNames: []string{"ValidateToken", "AuthenticateUser"}},
		{Category: "conceptual", Question: "How are a user's active sessions invalidated?", ExpectedNames: []string{"LogoutUser", "RevokeSession"}},
		{Category: "conceptual", Question: "How does the code determine whether an account is active?", ExpectedNames: []string{"IsUserActive", "FindUser"}},
		{Category: "conceptual", Question: "How is an email change persisted?", ExpectedNames: []string{"UpdateEmail", "UpdateUserEmail"}},
		{Category: "cross-file", Question: "Where is database-backed token validation called?", ExpectedNames: []string{"AuthenticateUser", "ValidateToken"}},
		{Category: "cross-file", Question: "Which service loads a user record from the database?", ExpectedNames: []string{"GetUser", "FindUser"}},
		{Category: "cross-file", Question: "Where does the service delegate an email write?", ExpectedNames: []string{"UpdateEmail", "UpdateUserEmail"}},
		{Category: "parent-child", Question: "Which AuthService methods handle login and logout?", ExpectedNames: []string{"AuthService", "AuthenticateUser", "LogoutUser"}},
		{Category: "parent-child", Question: "What methods are provided by UserService?", ExpectedNames: []string{"UserService", "GetUser", "UpdateEmail"}},
		{Category: "parent-child", Question: "Which Database methods work with tokens and sessions?", ExpectedNames: []string{"Database", "ValidateToken", "RevokeSession"}},
		{Category: "multi-chunk", Question: "What code runs during sign-in and where is the token checked?", ExpectedNames: []string{"AuthenticateUser", "ValidateToken"}},
		{Category: "multi-chunk", Question: "Where is a user fetched and what data shape is returned?", ExpectedNames: []string{"GetUser", "FindUser", "User"}},
		{Category: "multi-chunk", Question: "Which operations participate in changing a user's email?", ExpectedNames: []string{"UpdateEmail", "UpdateUserEmail"}},
		{Category: "vocabulary mismatch", Question: "How does login reject a credential that has been revoked?", ExpectedNames: []string{"AuthenticateUser", "ValidateToken"}},
		{Category: "vocabulary mismatch", Question: "How can an account be looked up using its numeric key?", ExpectedNames: []string{"GetUser", "FindUser", "User"}},
		{Category: "ambiguous", Question: "Where is the user?", ExpectedNames: []string{"GetUser", "FindUser"}},
		{Category: "negative", Question: "Where are password reset emails sent?"},
		{Category: "negative", Question: "Where are password hashes verified?"},
		{Category: "negative", Question: "Which function sends an email to the user?"},
		{Category: "negative", Question: "Where is the JWT signature verified?"},
		{Category: "negative", Question: "Which method deletes a user account?"},
	}
}

func TestRealRetrievalBenchmark(t *testing.T) {
	engine := realRetrievalEngine(t)
	chunks := make([]CodeChunk, len(engine.Documents))
	for i, document := range engine.Documents {
		chunks[i] = document.Chunk
	}
	relationshipGraph, err := BuildCodeRelationshipGraph("sample-project", chunks)
	if err != nil {
		t.Fatalf("BuildCodeRelationshipGraph() error = %v", err)
	}
	callRelationshipCount := 0
	parentMethodRelationshipCount := 0
	for _, relationship := range relationshipGraph.Relationships {
		if relationship.Kind == RelationshipCalls {
			callRelationshipCount++
		} else if relationship.Kind == RelationshipHasMethod {
			parentMethodRelationshipCount++
		}
		caller := relationshipGraph.chunksByID[relationship.FromChunkID]
		callee := relationshipGraph.chunksByID[relationship.ToChunkID]
		t.Logf("%s: %s -> %s", relationship.Kind, caller.Name, callee.Name)
	}
	t.Logf("Extracted %d function-call and %d parent-to-method relationships", callRelationshipCount, parentMethodRelationshipCount)
	tokenizer := CodeAwareTokenizer{}
	lexicalIndex, err := BuildLexicalIndex(chunks, tokenizer)
	if err != nil {
		t.Fatalf("BuildLexicalIndex() error = %v", err)
	}
	lexicalScorer := &LexicalScorer{Tokenizer: tokenizer, Index: lexicalIndex}

	const (
		evaluationK = 5
		rrfK        = 60
	)
	answerableCount := 0
	negativeCount := 0
	var semanticTotals, lexicalTotals, hybridTotals, callsTotals, structuralTotals benchmarkTotals
	categoryTotals := make(map[string]*categoryBenchmarkTotals)
	for _, benchmarkCase := range retrievalBenchmarkCases() {
		semantic, err := engine.Search(benchmarkCase.Question, evaluationK)
		if err != nil {
			t.Errorf("semantic Search(%q) error = %v", benchmarkCase.Question, err)
			continue
		}
		lexicalRanked, err := rankLexically(lexicalScorer, benchmarkCase.Question, chunks)
		if err != nil {
			t.Errorf("rankLexically(%q) error = %v", benchmarkCase.Question, err)
			continue
		}
		lexical := takeCodeSearchResults(asCodeSearchResults(lexicalRanked), evaluationK)
		hybrid, err := ReciprocalRankFusion(semantic, lexical, rrfK)
		if err != nil {
			t.Errorf("ReciprocalRankFusion(%q) error = %v", benchmarkCase.Question, err)
			continue
		}
		hybridResults := takeCodeSearchResults(hybridAsCodeSearchResults(hybrid), evaluationK)
		hybridCandidates := make([]CodeChunk, len(hybridResults))
		for i, result := range hybridResults {
			hybridCandidates[i] = result.Chunk
		}
		expandedChunks, err := relationshipGraph.ExpandCalls(hybridCandidates, evaluationK)
		if err != nil {
			t.Errorf("ExpandCalls(%q) error = %v", benchmarkCase.Question, err)
			continue
		}
		expandedResults := chunksAsCodeSearchResults(expandedChunks)
		structuralChunks, err := relationshipGraph.ExpandCallsAndChildren(hybridCandidates, evaluationK)
		if err != nil {
			t.Errorf("ExpandCallsAndChildren(%q) error = %v", benchmarkCase.Question, err)
			continue
		}
		structuralResults := chunksAsCodeSearchResults(structuralChunks)

		t.Logf("[%s] %s", benchmarkCase.Category, benchmarkCase.Question)
		if len(benchmarkCase.ExpectedNames) == 0 {
			negativeCount++
			semanticFalse := len(semantic) > 0
			lexicalFalse := len(lexical) > 0
			hybridFalse := len(hybridResults) > 0
			expandedFalse := len(expandedResults) > 0
			structuralFalse := len(structuralResults) > 0
			semanticTotals.negativeRetrieved += boolInt(semanticFalse)
			lexicalTotals.negativeRetrieved += boolInt(lexicalFalse)
			hybridTotals.negativeRetrieved += boolInt(hybridFalse)
			callsTotals.negativeRetrieved += boolInt(expandedFalse)
			structuralTotals.negativeRetrieved += boolInt(structuralFalse)
			t.Logf("negative false retrieval: Semantic=%t Lexical-v2=%t Hybrid=%t Hybrid+Calls=%t Hybrid+Structure=%t (any result in top %d)",
				semanticFalse, lexicalFalse, hybridFalse, expandedFalse, structuralFalse, evaluationK)
			logBenchmarkTopResults(t, "Semantic", semantic)
			logBenchmarkTopResults(t, "Lexical-v2", lexical)
			logBenchmarkTopResults(t, "Hybrid", hybridResults)
			logBenchmarkChunkResults(t, "Hybrid+Calls", expandedChunks)
			logBenchmarkChunkResults(t, "Hybrid+Structure", structuralChunks)
			continue
		}

		answerableCount++
		semanticMetrics := calculateBenchmarkMetrics(semantic, benchmarkCase.ExpectedNames)
		lexicalMetrics := calculateBenchmarkMetrics(lexical, benchmarkCase.ExpectedNames)
		hybridMetrics := calculateBenchmarkMetrics(hybridResults, benchmarkCase.ExpectedNames)
		expandedMetrics := calculateBenchmarkMetrics(expandedResults, benchmarkCase.ExpectedNames)
		structuralMetrics := calculateBenchmarkMetrics(structuralResults, benchmarkCase.ExpectedNames)
		semanticTotals.add(semanticMetrics)
		lexicalTotals.add(lexicalMetrics)
		hybridTotals.add(hybridMetrics)
		callsTotals.add(expandedMetrics)
		structuralTotals.add(structuralMetrics)
		category := categoryTotals[benchmarkCase.Category]
		if category == nil {
			category = &categoryBenchmarkTotals{}
			categoryTotals[benchmarkCase.Category] = category
		}
		category.count++
		category.semantic.add(semanticMetrics)
		category.lexical.add(lexicalMetrics)
		category.hybrid.add(hybridMetrics)
		category.expanded.add(expandedMetrics)
		category.structural.add(structuralMetrics)
		t.Logf("expected=%v", benchmarkCase.ExpectedNames)
		t.Logf("Semantic: %s", semanticMetrics.String())
		t.Logf("Lexical-v2: %s", lexicalMetrics.String())
		t.Logf("Hybrid: %s", hybridMetrics.String())
		t.Logf("Hybrid+Calls: %s", expandedMetrics.String())
		t.Logf("Hybrid+Structure: %s", structuralMetrics.String())
		logBenchmarkTopResults(t, "Semantic", semantic)
		logBenchmarkTopResults(t, "Lexical-v2", lexical)
		logBenchmarkTopResults(t, "Hybrid", hybridResults)
		logBenchmarkChunkResults(t, "Hybrid+Calls", expandedChunks)
		logBenchmarkChunkResults(t, "Hybrid+Structure", structuralChunks)
	}

	if answerableCount == 0 || negativeCount == 0 {
		t.Fatalf("benchmark has %d answerable and %d negative cases; want both", answerableCount, negativeCount)
	}
	t.Logf("Summary over %d answerable queries (Recall@1 / @3 / @5; MRR)", answerableCount)
	t.Logf("Semantic:  %s", semanticTotals.mean(answerableCount).String())
	t.Logf("Lexical-v2: %s", lexicalTotals.mean(answerableCount).String())
	t.Logf("Hybrid:    %s", hybridTotals.mean(answerableCount).String())
	t.Logf("Hybrid+Calls: %s", callsTotals.mean(answerableCount).String())
	t.Logf("Hybrid+Structure: %s", structuralTotals.mean(answerableCount).String())
	categoryNames := make([]string, 0, len(categoryTotals))
	for categoryName := range categoryTotals {
		categoryNames = append(categoryNames, categoryName)
	}
	sort.Strings(categoryNames)
	t.Log("Category breakdown (means over answerable queries):")
	t.Log("Category | N | System | Recall@1 | Recall@3 | Recall@5 | MRR")
	for _, categoryName := range categoryNames {
		category := categoryTotals[categoryName]
		for _, system := range []struct {
			name   string
			totals benchmarkTotals
		}{
			{name: "Semantic", totals: category.semantic},
			{name: "Lexical-v2", totals: category.lexical},
			{name: "Hybrid", totals: category.hybrid},
			{name: "Hybrid+Calls", totals: category.expanded},
			{name: "Hybrid+Structure", totals: category.structural},
		} {
			mean := system.totals.mean(category.count)
			t.Logf("%s | %d | %s | %.3f | %.3f | %.3f | %.3f",
				categoryName, category.count, system.name, mean.recall1, mean.recall3, mean.recall5, mean.mrr)
		}
	}
	t.Logf("Negative-query false retrieval rate (%d queries, any top-%d result): Semantic=%.3f (%d/%d), Lexical-v2=%.3f (%d/%d), Hybrid=%.3f (%d/%d), Hybrid+Calls=%.3f (%d/%d), Hybrid+Structure=%.3f (%d/%d)",
		negativeCount, evaluationK,
		float64(semanticTotals.negativeRetrieved)/float64(negativeCount), semanticTotals.negativeRetrieved, negativeCount,
		float64(lexicalTotals.negativeRetrieved)/float64(negativeCount), lexicalTotals.negativeRetrieved, negativeCount,
		float64(hybridTotals.negativeRetrieved)/float64(negativeCount), hybridTotals.negativeRetrieved, negativeCount,
		float64(callsTotals.negativeRetrieved)/float64(negativeCount), callsTotals.negativeRetrieved, negativeCount,
		float64(structuralTotals.negativeRetrieved)/float64(negativeCount), structuralTotals.negativeRetrieved, negativeCount)
}

type benchmarkMetrics struct {
	recall1 float64
	recall3 float64
	recall5 float64
	mrr     float64
}

func calculateBenchmarkMetrics(results []CodeSearchResult, expectedNames []string) benchmarkMetrics {
	found := make(map[string]bool, len(expectedNames))
	firstRelevantRank := 0
	var relevantAt1, relevantAt3, relevantAt5 int
	for i, result := range results {
		isRelevant := false
		for _, expected := range expectedNames {
			if result.Chunk.Name == expected {
				found[expected] = true
				isRelevant = true
			}
		}
		if isRelevant {
			if firstRelevantRank == 0 {
				firstRelevantRank = i + 1
			}
			if i < 1 {
				relevantAt1 = len(found)
			}
			if i < 3 {
				relevantAt3 = len(found)
			}
			if i < 5 {
				relevantAt5 = len(found)
			}
		}
	}
	denominator := float64(len(expectedNames))
	mrr := 0.0
	if firstRelevantRank > 0 {
		mrr = 1 / float64(firstRelevantRank)
	}
	return benchmarkMetrics{
		recall1: float64(relevantAt1) / denominator,
		recall3: float64(relevantAt3) / denominator,
		recall5: float64(relevantAt5) / denominator,
		mrr:     mrr,
	}
}

func (metrics benchmarkMetrics) String() string {
	return fmt.Sprintf("R@1=%.3f R@3=%.3f R@5=%.3f MRR=%.3f", metrics.recall1, metrics.recall3, metrics.recall5, metrics.mrr)
}

type benchmarkTotals struct {
	recall1           float64
	recall3           float64
	recall5           float64
	mrr               float64
	negativeRetrieved int
}

type categoryBenchmarkTotals struct {
	count      int
	semantic   benchmarkTotals
	lexical    benchmarkTotals
	hybrid     benchmarkTotals
	expanded   benchmarkTotals
	structural benchmarkTotals
}

func chunksAsCodeSearchResults(chunks []CodeChunk) []CodeSearchResult {
	results := make([]CodeSearchResult, len(chunks))
	for i, chunk := range chunks {
		results[i] = CodeSearchResult{Chunk: chunk}
	}
	return results
}

func (totals *benchmarkTotals) add(metrics benchmarkMetrics) {
	totals.recall1 += metrics.recall1
	totals.recall3 += metrics.recall3
	totals.recall5 += metrics.recall5
	totals.mrr += metrics.mrr
}

func (totals benchmarkTotals) mean(count int) benchmarkMetrics {
	denominator := float64(count)
	return benchmarkMetrics{
		recall1: totals.recall1 / denominator,
		recall3: totals.recall3 / denominator,
		recall5: totals.recall5 / denominator,
		mrr:     totals.mrr / denominator,
	}
}

func logBenchmarkTopResults(t *testing.T, label string, results []CodeSearchResult) {
	t.Helper()
	var names []string
	for i, result := range results {
		if i == 5 {
			break
		}
		names = append(names, fmt.Sprintf("%s(%.3f)", result.Chunk.Name, result.Score))
	}
	t.Logf("%s top results: %v", label, names)
}

func logBenchmarkChunkResults(t *testing.T, label string, chunks []CodeChunk) {
	t.Helper()
	var names []string
	for i, chunk := range chunks {
		if i == 5 {
			break
		}
		names = append(names, chunk.Name)
	}
	t.Logf("%s evidence: %v", label, names)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestCalculateBenchmarkMetrics(t *testing.T) {
	results := []CodeSearchResult{
		{Chunk: CodeChunk{Name: "Other"}},
		{Chunk: CodeChunk{Name: "ExpectedA"}},
		{Chunk: CodeChunk{Name: "Unrelated"}},
		{Chunk: CodeChunk{Name: "ExpectedB"}},
	}
	got := calculateBenchmarkMetrics(results, []string{"ExpectedA", "ExpectedB"})
	if got.recall1 != 0 || got.recall3 != 0.5 || got.recall5 != 1 || got.mrr != 0.5 {
		t.Fatalf("calculateBenchmarkMetrics() = %+v, want Recall@1=0 Recall@3=.5 Recall@5=1 MRR=.5", got)
	}
}
