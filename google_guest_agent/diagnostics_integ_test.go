//go:build integration
// +build integration

package main

import (
	"testing"
)

func TestDiagnosticsManager(t *testing.T) {
	var d = diagnosticsMgr{}
	if !d.disabled("linux") {
		t.Errorf("linux system does not support diagnose")
	}
}
