// Copyright 2024 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// OS file util for Google Guest Agent and Google Authorized Keys.

package utils

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// SaferWriteFile writes to a temporary file and then replaces the expected output file.
// This prevents other processes from reading partial content while the writer is still writing.
func SaferWriteFile(content []byte, outputFile string, perm fs.FileMode) error {
	dir := filepath.Dir(outputFile)
	name := filepath.Base(outputFile)

	if err := os.MkdirAll(dir, perm); err != nil {
		return fmt.Errorf("unable to create required directories %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, name+"*")
	if err != nil {
		return fmt.Errorf("unable to create temporary file under %q: %w", dir, err)
	}

	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return fmt.Errorf("unable to set permissions on temporary file %q: %w", dir, err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	if err := WriteFile(content, tmp.Name(), perm); err != nil {
		return fmt.Errorf("unable to write to a temporary file %q: %w", tmp.Name(), err)
	}

	return os.Rename(tmp.Name(), outputFile)
}

// CopyFile copies content from src to dst and sets permissions.
func CopyFile(src, dst string, perm fs.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read %q: %w", src, err)
	}

	if err := WriteFile(b, dst, perm); err != nil {
		return fmt.Errorf("failed to write %q: %w", dst, err)
	}

	if err := os.Chmod(dst, perm); err != nil {
		return fmt.Errorf("unable to set permissions on destination file %q: %w", dst, err)
	}

	return nil
}

// WriteFile creates parent directories if required and writes content to the output file.
func WriteFile(content []byte, outputFile string, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(outputFile), perm); err != nil {
		return fmt.Errorf("unable to create required directories for %q: %w", outputFile, err)
	}
	return os.WriteFile(outputFile, content, perm)
}
