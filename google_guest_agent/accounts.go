package main

type UserManager interface {
	resetPwd(username, pwd string) error
	getUID(string) string
	createUser(string, string) error
	addUserToGroup(user, group string) error
	userExists(name string) (bool, error)
}
