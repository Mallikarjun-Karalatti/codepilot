package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

type Tokenizer interface {
	Tokenize(text string) []string
}

type CodeAwareTokenizer struct{}

var (
	acronymBoundary  = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	camelBoundary    = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	lexicalStopwords = map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {},
		"been": {}, "being": {}, "by": {}, "does": {}, "do": {}, "for": {}, "from": {},
		"how": {}, "in": {}, "into": {}, "is": {}, "it": {}, "of": {}, "on": {},
		"or": {}, "s": {}, "that": {}, "the": {}, "this": {}, "to": {}, "was": {},
		"were": {}, "what": {}, "when": {}, "where": {}, "which": {}, "who": {},
		"with": {},
	}
)

func (CodeAwareTokenizer) Tokenize(text string) []string {
	text = acronymBoundary.ReplaceAllString(text, `${1} ${2}`)
	text = camelBoundary.ReplaceAllString(text, `${1} ${2}`)
	var tokens []string
	var token strings.Builder
	flush := func() {
		if token.Len() == 0 {
			return
		}
		if normalized := normalizeLexicalToken(token.String()); normalized != "" {
			if _, stopword := lexicalStopwords[normalized]; !stopword {
				tokens = append(tokens, normalized)
			}
		}
		token.Reset()
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			token.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

func normalizeLexicalToken(token string) string {
	token = strings.ToLower(token)
	// These common inflections share a stem, so "authentication" and
	// "AuthenticateUser" can match after tokenization.
	if strings.HasPrefix(token, "authentic") {
		return "authentic"
	}
	switch token {
	case "updated", "updating", "updates":
		return "update"
	}
	if strings.HasSuffix(token, "ies") && len(token) > 4 {
		token = strings.TrimSuffix(token, "ies") + "y"
	} else if strings.HasSuffix(token, "s") && !strings.HasSuffix(token, "ss") && len(token) > 3 {
		token = strings.TrimSuffix(token, "s")
	}
	return token
}

type LexicalScorer struct {
	Tokenizer Tokenizer
}

type LexicalMatchDetails struct {
	NameMatches       []string
	ParentNameMatches []string
	TextMatches       []string
}

func (scorer *LexicalScorer) Score(
	query string,
	chunk CodeChunk,
) (float64, LexicalMatchDetails, error) {
	if scorer == nil {
		return 0, LexicalMatchDetails{}, fmt.Errorf("lexical scorer must not be nil")
	}
	if scorer.Tokenizer == nil {
		return 0, LexicalMatchDetails{}, fmt.Errorf("tokenizer must not be nil")
	}

	queryTokens := uniqueTokens(scorer.Tokenizer.Tokenize(query))
	nameMatches := matchingTokens(queryTokens, scorer.Tokenizer.Tokenize(chunk.Name))
	parentMatches := matchingTokens(queryTokens, scorer.Tokenizer.Tokenize(chunk.ParentName))
	textMatches := matchingTokens(queryTokens, scorer.Tokenizer.Tokenize(chunk.Text))

	const (
		nameWeight       = 5.0
		parentNameWeight = 2.0
		textWeight       = 1.0
	)
	score := float64(len(nameMatches))*nameWeight +
		float64(len(parentMatches))*parentNameWeight +
		float64(len(textMatches))*textWeight

	return score, LexicalMatchDetails{
		NameMatches:       nameMatches,
		ParentNameMatches: parentMatches,
		TextMatches:       textMatches,
	}, nil
}

func uniqueTokens(tokens []string) []string {
	seen := make(map[string]struct{}, len(tokens))
	unique := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		unique = append(unique, token)
	}
	return unique
}

func matchingTokens(queryTokens, fieldTokens []string) []string {
	fieldSet := make(map[string]struct{}, len(fieldTokens))
	for _, token := range fieldTokens {
		fieldSet[token] = struct{}{}
	}
	matches := make([]string, 0)
	for _, token := range queryTokens {
		if _, ok := fieldSet[token]; ok {
			matches = append(matches, token)
		}
	}
	return matches
}
