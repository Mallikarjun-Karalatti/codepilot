package main

import (
	"math"
	"reflect"
	"testing"
)

func TestCodeAwareTokenizer(t *testing.T) {
	tokenizer := CodeAwareTokenizer{}
	got := tokenizer.Tokenize("authentication emails AuthenticateUser UpdateUserEmail UserService snake_case SCREAMING_SNAKE_CASE")
	want := []string{
		"authentic", "email", "authentic", "user", "update", "user", "email",
		"user", "service", "snake", "case", "screaming", "snake", "case",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tokenize() = %#v, want %#v", got, want)
	}
}

func TestLexicalScorerWeightsFieldsAndNormalizesTokens(t *testing.T) {
	scorer := &LexicalScorer{Tokenizer: CodeAwareTokenizer{}}
	chunk := CodeChunk{
		Name:       "AuthenticateUser",
		ParentName: "AuthService",
		Text:       "func (a *AuthService) AuthenticateUser(token string) bool { return true }",
	}

	score, details, err := scorer.Score("authentication", chunk)
	if err != nil {
		t.Fatalf("Score() error = %v", err)
	}
	if score != 6 {
		t.Fatalf("Score() = %v, want 6 (name weight 5 + text weight 1)", score)
	}
	if !reflect.DeepEqual(details.NameMatches, []string{"authentic"}) {
		t.Errorf("NameMatches = %#v, want [authentic]", details.NameMatches)
	}
	if len(details.ParentNameMatches) != 0 {
		t.Errorf("ParentNameMatches = %#v, want no matches", details.ParentNameMatches)
	}
	if !reflect.DeepEqual(details.TextMatches, []string{"authentic"}) {
		t.Errorf("TextMatches = %#v, want [authentic]", details.TextMatches)
	}
}

func TestLexicalScorerMatchesParentName(t *testing.T) {
	scorer := &LexicalScorer{Tokenizer: CodeAwareTokenizer{}}
	score, details, err := scorer.Score("UserService", CodeChunk{ParentName: "UserService"})
	if err != nil {
		t.Fatalf("Score() error = %v", err)
	}
	if score != 4 {
		t.Fatalf("Score() = %v, want 4", score)
	}
	if !reflect.DeepEqual(details.ParentNameMatches, []string{"user", "service"}) {
		t.Errorf("ParentNameMatches = %#v, want [user service]", details.ParentNameMatches)
	}
}

func TestLexicalScorerRequiresTokenizer(t *testing.T) {
	if _, _, err := (&LexicalScorer{}).Score("query", CodeChunk{}); err == nil {
		t.Fatal("Score() error = nil, want missing tokenizer error")
	}
}

func TestBuildLexicalIndexCountsDocumentFrequencyOncePerChunk(t *testing.T) {
	tokenizer := CodeAwareTokenizer{}
	chunks := []CodeChunk{
		{Name: "User", ParentName: "User", Text: "User user"},
		{Name: "UserService", Text: "func (u *UserService) GetUser() {}"},
		{Name: "AuthenticateToken", Text: "authentication"},
	}

	index, err := BuildLexicalIndex(chunks, tokenizer)
	if err != nil {
		t.Fatalf("BuildLexicalIndex() error = %v", err)
	}
	if index.DocumentCount != 3 {
		t.Errorf("DocumentCount = %d, want 3", index.DocumentCount)
	}
	if got := index.DocumentFrequency["user"]; got != 2 {
		t.Errorf("DocumentFrequency[user] = %d, want 2", got)
	}
	if got := index.DocumentFrequency["authentic"]; got != 1 {
		t.Errorf("DocumentFrequency[authentic] = %d, want 1", got)
	}
	if index.IDF("authentic") <= index.IDF("user") {
		t.Errorf("IDF(authentic) = %v, IDF(user) = %v; want rare term to weigh more", index.IDF("authentic"), index.IDF("user"))
	}
	wantRareIDF := math.Log(4.0 / 2.0)
	if got := index.IDF("authentic"); math.Abs(got-wantRareIDF) > 1e-12 {
		t.Errorf("IDF(authentic) = %v, want %v", got, wantRareIDF)
	}
}

func TestBuildLexicalIndexRequiresTokenizer(t *testing.T) {
	if _, err := BuildLexicalIndex(nil, nil); err == nil {
		t.Fatal("BuildLexicalIndex() error = nil, want missing tokenizer error")
	}
}
