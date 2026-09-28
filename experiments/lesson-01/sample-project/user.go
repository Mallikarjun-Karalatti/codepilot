package sampleproject

type User struct {
	ID     int
	Email  string
	Active bool
}

type UserService struct {
	DB *Database
}

func (s *UserService) GetUser(id int) (User, bool) {
	return s.DB.FindUser(id)
}

func (s *UserService) UpdateEmail(id int, email string) bool {
	return s.DB.UpdateUserEmail(id, email)
}

func IsUserActive(user User) bool {
	return user.Active
}
