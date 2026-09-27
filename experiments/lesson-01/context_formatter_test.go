package main

import "testing"

func TestContextFormatterFormat(t *testing.T) {
	chunks := []CodeChunk{
		{
			Kind:       ChunkKindStruct,
			Name:       "UserService",
			SourceFile: "services/user_service.go",
			StartLine:  3,
			EndLine:    5,
			Text: `type UserService struct {
    DB *Database
}`,
		},
		{
			Kind:       ChunkKindMethod,
			Name:       "AuthenticateUser",
			ParentName: "UserService",
			SourceFile: "services/user_service.go",
			StartLine:  7,
			EndLine:    9,
			Text: `func (s *UserService) AuthenticateUser(token string) bool {
    return token != ""
}`,
		},
	}

	want := `[STRUCT: UserService]
Source: services/user_service.go:3-5
type UserService struct {
    DB *Database
}

[METHOD: AuthenticateUser]
Source: services/user_service.go:7-9
Parent: UserService
func (s *UserService) AuthenticateUser(token string) bool {
    return token != ""
}`
	got := (&ContextFormatter{}).Format(chunks)
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestContextFormatterFormatEmpty(t *testing.T) {
	got := (&ContextFormatter{}).Format(nil)
	if got != "" {
		t.Errorf("Format(nil) = %q, want empty string", got)
	}
}
