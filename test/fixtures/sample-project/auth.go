package sampleproject

import "strings"

// added a comment to test the relationship graph
type AuthService struct {
	DB *Database
}

func (a *AuthService) AuthenticateUser(token string) bool {
	// Validate the JWT token with the local database-backed auth store.
	return token != "" && a.DB.ValidateToken(token)
}

func (a *AuthService) LogoutUser(userID int) {
	// Revoke the user's active session.
	a.DB.RevokeSession(userID)
}

func NormalizeToken(token string) string {
	return strings.TrimSpace(token)
}
