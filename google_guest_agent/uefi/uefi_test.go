package uefi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVariablePath(t *testing.T) {
	v := VariableName{Name: "name", GUID: "guid"}
	want := "/sys/firmware/efi/efivars/name-guid"

	if got := VariablePath(v); got != want {
		t.Errorf("VariablePath(%+v) = %v, want %v", v, got, want)
	}
}

func TestReadVariable(t *testing.T) {
	v := VariableName{Name: "testname", GUID: "testguid"}
	fakecert := `
	-----BEGIN CERTIFICATE-----
	sdfsd
	-----END CERTIFICATE-----
	`
	fakeUefi := []byte("attr" + fakecert)
	path := filepath.Join(os.TempDir(), "testname-testguid")

	if err := os.WriteFile(path, fakeUefi, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	defer os.Remove(path)

	got, err := ReadVariable(v)

	if err != nil {
		t.Errorf("ReadVariable(%+v) failed unexpectedly with error: %v", v, err)
	}

	if string(got.Attributes) != "attr" {
		t.Errorf("ReadVariable(%+v) = %s as attributes, want %s", v, string(got.Attributes), "attr")
	}
	if string(got.Content) != fakecert {
		t.Errorf("ReadVariable(%+v) = %s as content, want %s", v, string(got.Content), fakeUefi)
	}
}

func TestReadVariableError(t *testing.T) {
	v := VariableName{Name: "testname", GUID: "testguid"}
	p := filepath.Join(os.TempDir(), "testname-testguid")

	// File not exist error.
	_, err := ReadVariable(v)
	if err == nil {
		t.Errorf("ReadVariable(%+v) succeeded for non-existent file, want error", v)
	}

	// Empty variable error.
	os.WriteFile(p, []byte(""), 0644)

	_, err = ReadVariable(v)
	if err == nil {
		t.Errorf("ReadVariable(%+v) succeeded for invalid format, want error", v)
	}
}
