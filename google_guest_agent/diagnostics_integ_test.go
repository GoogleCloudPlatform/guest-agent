package main

import (
	"testing"
)

func TestDiagnosticsManager(t *testing.T) {
	var d = diagnosticsMgr{}
	if !d.disabled("linux") {
		t.Fatalf("linux system does not support diagnose")
	}
}
