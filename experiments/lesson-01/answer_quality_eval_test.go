package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const answerEvaluationMaxTokens = 6000

func TestRealAnswerQualityWithAndWithoutStructuralEvidence(t *testing.T) {
	engine := realRetrievalEngine(t)
	const (
		evaluationK = 5
		rrfK        = 60
	)
	embedder := engine.Embedder.(*EmbeddingClient)
	chunks := make([]CodeChunk, len(engine.Documents))
	chunksByName := make(map[string][]CodeChunk, len(engine.Documents))
	for i, document := range engine.Documents {
		chunks[i] = document.Chunk
		chunksByName[document.Chunk.Name] = append(chunksByName[document.Chunk.Name], document.Chunk)
	}

	graph, err := BuildCodeRelationshipGraph("sample-project", chunks)
	if err != nil {
		t.Fatalf("BuildCodeRelationshipGraph() error = %v", err)
	}
	tokenizer := CodeAwareTokenizer{}
	lexicalIndex, err := BuildLexicalIndex(chunks, tokenizer)
	if err != nil {
		t.Fatalf("BuildLexicalIndex() error = %v", err)
	}
	lexicalScorer := &LexicalScorer{Tokenizer: tokenizer, Index: lexicalIndex}
	baseRetriever := &HybridEvidenceRetriever{
		SemanticSearch: engine,
		LexicalScorer:  lexicalScorer,
		Chunks:         chunks,
		Selector:       &EvidenceSelector{MaxChunks: evaluationK, Policy: EvidencePolicyDirectFirst},
		CandidateLimit: evaluationK,
		RRFK:           rrfK,
	}
	structuralRetriever := *baseRetriever
	structuralRetriever.Graph = graph
	tokenCounter := &answerEvalTokenCounter{counter: embedder, counts: make(map[string]int)}
	contextBuilder := &ContextBuilder{
		Documents: chunks,
		Tokenizer: tokenCounter,
		MaxTokens: answerEvaluationMaxTokens,
	}
	formatter := &ContextFormatter{}
	llm := NewLLMClient("http://localhost:11434", "qwen3:8b")
	llm.Options = map[string]any{"temperature": 0.0}
	llm.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	hybridAssistant := &CodeAssistant{Retriever: baseRetriever, ContextBuilder: contextBuilder, Formatter: formatter, LLM: llm}
	directFirstAssistant := &CodeAssistant{Retriever: &structuralRetriever, ContextBuilder: contextBuilder, Formatter: formatter, LLM: llm}

	evaluationCases := retrievalBenchmarkCases()
	startIndex := 0
	if value := os.Getenv("CODEPILOT_ANSWER_EVAL_START"); value != "" {
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil || parsed < 0 || parsed > len(evaluationCases) {
			t.Fatalf("CODEPILOT_ANSWER_EVAL_START=%q must be an integer from 0 through %d", value, len(evaluationCases))
		}
		startIndex = parsed
	}
	evaluationCases = evaluationCases[startIndex:]
	if value := os.Getenv("CODEPILOT_ANSWER_EVAL_COUNT"); value != "" {
		count, parseErr := strconv.Atoi(value)
		if parseErr != nil || count < 0 {
			t.Fatalf("CODEPILOT_ANSWER_EVAL_COUNT=%q must be a non-negative integer", value)
		}
		if count < len(evaluationCases) {
			evaluationCases = evaluationCases[:count]
		}
	}
	t.Logf("Running answer-quality cases %d-%d of the %d-query benchmark", startIndex+1, startIndex+len(evaluationCases), len(retrievalBenchmarkCases()))
	failedPairs := 0
	for _, benchmarkCase := range evaluationCases {
		hybridAnswer, hybridEvidence, err := hybridAssistant.AskWithEvidence(benchmarkCase.Question)
		if err != nil {
			t.Errorf("CodeAssistant.AskWithEvidence(Hybrid, %q) error = %v; skipping this answer pair", benchmarkCase.Question, err)
			failedPairs++
			continue
		}
		directFirstAnswer, directFirstEvidence, err := directFirstAssistant.AskWithEvidence(benchmarkCase.Question)
		if err != nil {
			t.Errorf("CodeAssistant.AskWithEvidence(Direct-First, %q) error = %v; answer pair is incomplete", benchmarkCase.Question, err)
			failedPairs++
			continue
		}
		if strings.TrimSpace(hybridAnswer) == "" || strings.TrimSpace(directFirstAnswer) == "" {
			t.Errorf("LLM returned an empty answer for %q; answer pair is incomplete", benchmarkCase.Question)
			failedPairs++
			continue
		}

		hybridContextChunks := evidenceChunks(hybridEvidence)
		directFirstContextChunks := evidenceChunks(directFirstEvidence)
		hybridEval := assessAnswer(benchmarkCase, hybridAnswer, hybridContextChunks, chunksByName)
		directFirstEval := assessAnswer(benchmarkCase, directFirstAnswer, directFirstContextChunks, chunksByName)
		t.Logf("\n[%s] Question: %s", benchmarkCase.Category, benchmarkCase.Question)
		t.Logf("Settings: model=%s embedding=qwen3-embedding temperature=%.1f MaxTokens=%d topK=%d RRF-k=%d", llm.Model, llm.Options["temperature"], answerEvaluationMaxTokens, evaluationK, rrfK)
		t.Logf("Hybrid context chunks: %s", chunkNames(hybridContextChunks))
		t.Logf("Direct-First context chunks: %s", chunkNames(directFirstContextChunks))
		t.Logf("Hybrid answer:\n%s", hybridAnswer)
		t.Logf("Hybrid deterministic checks: %s", hybridEval.String())
		t.Logf("Direct-First answer:\n%s", directFirstAnswer)
		t.Logf("Direct-First deterministic checks: %s", directFirstEval.String())
	}
	t.Logf("Answer-quality run completed with %d failed or incomplete pairs out of %d selected questions", failedPairs, len(evaluationCases))
	if failedPairs > 0 {
		t.Errorf("answer-quality run had %d failed or incomplete pairs", failedPairs)
	}
}

func evidenceChunks(evidence []EvidenceCandidate) []CodeChunk {
	chunks := make([]CodeChunk, len(evidence))
	for i, candidate := range evidence {
		chunks[i] = candidate.Chunk
	}
	return chunks
}

type answerEvalTokenCounter struct {
	counter TokenCounter
	counts  map[string]int
}

func (counter *answerEvalTokenCounter) CountTokens(text string) (int, error) {
	if count, ok := counter.counts[text]; ok {
		return count, nil
	}
	count, err := counter.counter.CountTokens(text)
	if err != nil {
		return 0, err
	}
	counter.counts[text] = count
	return count, nil
}

type answerEval struct {
	ExpectedMentioned []string
	ExpectedTotal     int
	UnsupportedNames  []string
	CorrectLocations  []string
	LocationTotal     int
	Abstained         bool
	Negative          bool
}

func assessAnswer(testCase RetrievalBenchmarkCase, answer string, contextChunks []CodeChunk, chunksByName map[string][]CodeChunk) answerEval {
	lowerAnswer := strings.ToLower(answer)
	contextNames := make(map[string]struct{}, len(contextChunks))
	for _, chunk := range contextChunks {
		contextNames[chunk.Name] = struct{}{}
	}

	evaluation := answerEval{
		ExpectedTotal: len(testCase.ExpectedNames),
		LocationTotal: len(testCase.ExpectedNames),
		Negative:      len(testCase.ExpectedNames) == 0,
	}
	for _, name := range testCase.ExpectedNames {
		if !containsIdentifier(answer, name) {
			continue
		}
		evaluation.ExpectedMentioned = append(evaluation.ExpectedMentioned, name)
		for _, chunk := range chunksByName[name] {
			if _, grounded := contextNames[name]; !grounded {
				evaluation.UnsupportedNames = appendUnique(evaluation.UnsupportedNames, name)
				continue
			}
			if strings.Contains(answer, filepath.Base(chunk.SourceFile)) {
				evaluation.CorrectLocations = appendUnique(evaluation.CorrectLocations, name)
			}
		}
	}
	for name := range chunksByName {
		if containsIdentifier(answer, name) {
			if _, grounded := contextNames[name]; !grounded {
				evaluation.UnsupportedNames = appendUnique(evaluation.UnsupportedNames, name)
			}
		}
	}
	evaluation.Abstained = containsAbstentionPhrase(lowerAnswer)
	return evaluation
}

func containsIdentifier(text, identifier string) bool {
	pattern := `(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(identifier) + `([^A-Za-z0-9_]|$)`
	matched, _ := regexp.MatchString(pattern, text)
	return matched
}

func (evaluation answerEval) String() string {
	unsupportedNames := append([]string(nil), evaluation.UnsupportedNames...)
	sort.Strings(unsupportedNames)
	completeness := "n/a"
	if evaluation.ExpectedTotal > 0 {
		completeness = fmt.Sprintf("%d/%d expected identifiers stated", len(evaluation.ExpectedMentioned), evaluation.ExpectedTotal)
	}
	locations := "n/a"
	if evaluation.LocationTotal > 0 {
		locations = fmt.Sprintf("%d/%d expected identifiers with correct source file named", len(evaluation.CorrectLocations), evaluation.LocationTotal)
	}
	abstention := "n/a"
	if evaluation.Negative {
		abstention = fmt.Sprintf("%t", evaluation.Abstained)
	}
	return fmt.Sprintf("correctness=manual review; completeness=%s; groundedness=unsupported repository identifiers %v; abstention=%s; citation/location=%s",
		completeness, unsupportedNames, abstention, locations)
}

func containsAbstentionPhrase(answer string) bool {
	for _, phrase := range []string{
		"not present", "not implemented", "does not exist", "doesn't exist", "does not contain", "doesn't contain", "does not include", "doesn't include", "does not mention", "doesn't mention", "not verified in the given code",
		"couldn't find", "cannot find", "not found", "no code", "isn't implemented",
		"none of the provided", "none of the code", "no mention of", "there is no mention",
	} {
		if strings.Contains(answer, phrase) {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func chunkNames(chunks []CodeChunk) string {
	names := make([]string, len(chunks))
	for i, chunk := range chunks {
		names[i] = chunk.Name
	}
	return strings.Join(names, ", ")
}
