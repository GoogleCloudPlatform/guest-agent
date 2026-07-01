package cfg

import (
	"strings"
	"testing"
)

func TestUserCmdsSanity(t *testing.T) {
	if err := Load(nil); err != nil {
		t.Fatalf("Failed to load configuration: %+v", err)
	}

	c := Get()

	if !strings.HasPrefix(c.Accounts.GPasswdAddCmd, "pw ") ||
		!strings.HasPrefix(c.Accounts.GPasswdRemoveCmd, "pw ") ||
		!strings.HasPrefix(c.Accounts.UserAddCmd, "pw ") ||
		!strings.HasPrefix(c.Accounts.GroupAddCmd, "pw ") ||
		!strings.HasPrefix(c.Accounts.UserDelCmd, "pw ") {
		t.Errorf("FreeBSD uses the pw command to manage users and groups")
	}
}
