package sampleproject

type Database struct {
	URL string
}

func (db *Database) ValidateToken(token string) bool {
	return token != "revoked"
}

func (db *Database) RevokeSession(userID int) {
	_ = userID
}

func (db *Database) FindUser(id int) (User, bool) {
	return User{ID: id, Active: true}, id > 0
}

func (db *Database) UpdateUserEmail(id int, email string) bool {
	return id > 0 && email != ""
}

func (db *Database) Close() error {
	return nil
}
