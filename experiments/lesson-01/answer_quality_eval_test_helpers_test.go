package main

import "testing"

func TestAssessAnswerDeterministicSignals(t *testing.T) {
	if containsIdentifier("UserService.GetUser", "User") {
		t.Fatal("containsIdentifier() matched User inside UserService")
	}
	if !containsIdentifier("The User struct is returned", "User") {
		t.Fatal("containsIdentifier() did not match standalone User identifier")
	}
	chunksByName := map[string][]CodeChunk{
		"AuthenticateUser": {{Name: "AuthenticateUser", SourceFile: "auth.go"}},
		"ValidateToken":    {{Name: "ValidateToken", SourceFile: "database.go"}},
	}
	context := []CodeChunk{chunksByName["AuthenticateUser"][0], chunksByName["ValidateToken"][0]}

	t.Run("complete grounded answer with locations", func(t *testing.T) {
		caseData := RetrievalBenchmarkCase{ExpectedNames: []string{"AuthenticateUser", "ValidateToken"}}
		got := assessAnswer(caseData, "AuthenticateUser in auth.go calls ValidateToken in database.go.", context, chunksByName)
		if len(got.ExpectedMentioned) != 2 || len(got.UnsupportedNames) != 0 || len(got.CorrectLocations) != 2 {
			t.Fatalf("assessAnswer() = %+v, want complete grounded answer with both locations", got)
		}
	})

	t.Run("unsupported symbol", func(t *testing.T) {
		caseData := RetrievalBenchmarkCase{ExpectedNames: []string{"AuthenticateUser"}}
		got := assessAnswer(caseData, "AuthenticateUser calls ValidateToken.", context[:1], chunksByName)
		if len(got.UnsupportedNames) != 1 || got.UnsupportedNames[0] != "ValidateToken" {
			t.Fatalf("unsupported names = %v, want [ValidateToken]", got.UnsupportedNames)
		}
	})

	t.Run("negative answer abstention phrase", func(t *testing.T) {
		caseData := RetrievalBenchmarkCase{Question: "Where are password reset emails sent?"}
		got := assessAnswer(caseData, "Password reset email sending is not present in this repository.", nil, chunksByName)
		if !got.Abstained {
			t.Fatal("assessAnswer() marked explicit negative answer as not abstaining")
		}
		for _, answer := range []string{
			"None of the provided functions send an email to the user.",
			"There is no mention of password hash verification in the code.",
		} {
			if !assessAnswer(caseData, answer, nil, chunksByName).Abstained {
				t.Errorf("assessAnswer(%q) marked a negative answer as not abstaining", answer)
			}
		}
	})
}
