//go:build integration
// +build integration

package main

import (
	"testing"
)

func TestGetRelease(t *testing.T) {
	osrelease := getRelease()
	if osrelease.os == "" {
		t.Errorf("failed to get os name")
	}
	if osrelease.version.String() == "" {
		t.Errorf("failed to get os version")
	}
}
