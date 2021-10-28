// +build integration

package main

import (
	"testing"
	"time"
)

const (
	malformedKey  = "malformed-ssh-keys";
	malformedKey2 = ":malformed-ssh-keys";
)

func TestMalformedSSHKeys(t *testing.T) {
	// insert a malformed ssh keys
	newMetadata.Instance.Attributes.SSHKeys = append(newMetadata.Instance.Attributes.SSHKeys, malformedKey)
	time.Sleep(5 * time.Second)
	if exist, err := userExists(malformedKey); exist {
		t.Fatalf("%s user should not exist", malformedKey)
	}

	newMetadata.Instance.Attributes.SSHKeys = append(newMetadata.Instance.Attributes.SSHKeys, malformedKey2)
	time.Sleep(5 * time.Second)
	if exist, err := userExists(""); exist {
		t.Fatalf("user with empty name should not exist")
	}

	newMetadata.Instance.Attributes.SSHKeys = append(newMetadata.Instance.Attributes.SSHKeys, malformedKey2)
	time.Sleep(5 * time.Second)
	if exist, err := userExists(""); exist {
		t.Fatalf("user with empty name should not exist")
	}
}
