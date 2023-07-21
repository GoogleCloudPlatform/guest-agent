package agentcrypto

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/uefi"
)

func TestReadAndWriteRootCACert(t *testing.T) {
	v := uefi.VariableName{Name: "testname", GUID: "testguid"}

	fakeUefi := []byte("attr" + validCertPEM)
	path := filepath.Join(os.TempDir(), "testname-testguid")

	if err := os.WriteFile(path, fakeUefi, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	defer os.Remove(path)

	crt := filepath.Join(os.TempDir(), "root.crt")
	if err := readAndWriteRootCACert(v, crt); err != nil {
		t.Errorf("readAndWriteRootCACert(%+v, %s) failed unexpectedly with error: %v", v, crt, err)
	}

	got, err := os.ReadFile(crt)
	if err != nil {
		t.Errorf("Failed to read expected root cert file: %v", err)
	}
	if string(got) != validCertPEM {
		t.Errorf("readAndWriteRootCACert(%+v, %s) = %s, want %s", v, crt, string(got), validCertPEM)
	}
}

func TestReadAndWriteRootCACertError(t *testing.T) {
	v := uefi.VariableName{Name: "not", GUID: "exist"}

	crt := filepath.Join(os.TempDir(), "root.crt")
	// Non-existent UEFI variable.
	if err := readAndWriteRootCACert(v, crt); err == nil {
		t.Errorf("readAndWriteRootCACert(%+v, %s) succeeded unexpectedly for non-existent UEFI variable, want error", v, crt)
	}

	// Invalid PEM certificate.
	fakeUefi := []byte("attr" + invalidCertPEM)
	path := filepath.Join(os.TempDir(), "testname-testguid")

	if err := os.WriteFile(path, fakeUefi, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	defer os.Remove(path)

	if err := readAndWriteRootCACert(v, crt); err == nil {
		t.Errorf("readAndWriteRootCACert(%+v, %s) succeeded unexpectedly for invalid PEM certificate, want error", v, crt)
	}
}
