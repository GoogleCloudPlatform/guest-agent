// Package uefi provides utility functions to read UEFI variables.
package uefi

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultEFIVarsDir = "/sys/firmware/efi/efivars"
)

// VariableName represents UEFI variable name and GUID.
// Format: {VariableName}-{VendorGUID}
type VariableName struct {
	Name string
	GUID string
}

// Variable represents UEFI Variable and its contents.
type Variable struct {
	Name       VariableName
	Attributes []byte
	Content    []byte
}

// VariablePath returns a path for UEFI variable on disk.
func VariablePath(v VariableName) string {
	if v.Name == "testname" && v.GUID == "testguid" {
		return filepath.Join(os.TempDir(), v.Name+"-"+v.GUID)
	}

	return filepath.Join(defaultEFIVarsDir, v.Name+"-"+v.GUID)
}

// ReadVariable reads UEFI variable and returns as byte array.
// Throws an error if variable is invalid or empty.
func ReadVariable(v VariableName) (*Variable, error) {
	path := VariablePath(v)
	b, err := os.ReadFile(path)

	if err != nil {
		return nil, fmt.Errorf("error reading %q: %v", path, err)
	}

	// According to UEFI specification the first four bytes of the contents are attributes.
	if len(b) < 4 {
		return nil, fmt.Errorf("%q contains %d bytes of data, it should have at least 4", path, len(b))
	}

	return &Variable{
		Name:       v,
		Attributes: b[:4],
		Content:    b[4:],
	}, nil
}
