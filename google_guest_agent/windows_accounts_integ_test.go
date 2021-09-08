// +build windows

package main

import (
	"testing"
)

const testUser = "integration-test-user"

func TestCreateOrResetPwd(t *testing.T) {
	var key = &windowsKey{
		Email:               "test-email@google.com",
		UserName:            testUser,
		AddToAdministrators: mkptr(true),
		PasswordLength:      15,
	}
	if exist, _ := userExists(testUser); exist == true {
		t.Errorf("test user should not exist")
	}
	if _, err := key.createOrResetPwd(); err != nil {
		t.Errorf("failed creating test user")
	}
	if exist, _ := userExists(testUser); exist != true {
		t.Errorf("test user should exist")
	}
	if exist, _ := userExistsInGroup(testUser, "Administrators"); exist != true {
		t.Errorf("test user should exist in group Administrators")
	}
}
