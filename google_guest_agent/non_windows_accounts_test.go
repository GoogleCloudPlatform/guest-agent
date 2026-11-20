// Copyright 2026 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/sys/unix"
)

// withStubUserExists replaces userExistsFunc for the duration of a test and
// restores it afterwards.
func withStubUserExists(t *testing.T, stub func(string) (bool, error)) {
	t.Helper()
	orig := userExistsFunc
	userExistsFunc = stub
	t.Cleanup(func() { userExistsFunc = orig })
}

// withFastPolling shrinks the poll interval so tests don't have to wait out
// the real production backoff, and restores the original values afterwards.
func withFastPolling(t *testing.T, attempts int) {
	t.Helper()
	origInterval, origAttempts := userVisiblePollInterval, userVisiblePollAttempts
	userVisiblePollInterval = time.Millisecond
	userVisiblePollAttempts = attempts
	t.Cleanup(func() {
		userVisiblePollInterval = origInterval
		userVisiblePollAttempts = origAttempts
	})
}

func TestWaitForUserVisibleImmediateSuccess(t *testing.T) {
	calls := 0
	withStubUserExists(t, func(string) (bool, error) {
		calls++
		return true, nil
	})
	withFastPolling(t, 5)

	if err := waitForUserVisible(context.Background(), "testuser"); err != nil {
		t.Fatalf("waitForUserVisible() = %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("userExistsFunc called %d times, want 1 (no retry needed)", calls)
	}
}

func TestWaitForUserVisibleEventualSuccess(t *testing.T) {
	calls := 0
	withStubUserExists(t, func(string) (bool, error) {
		calls++
		if calls < 3 {
			// Mirrors the observed nscd race: the account was just
			// created but a lookup transiently reports it missing.
			return false, errors.New("user: unknown user testuser")
		}
		return true, nil
	})
	withFastPolling(t, 5)

	if err := waitForUserVisible(context.Background(), "testuser"); err != nil {
		t.Fatalf("waitForUserVisible() = %v, want nil", err)
	}
	if calls != 3 {
		t.Errorf("userExistsFunc called %d times, want 3", calls)
	}
}

func TestWaitForUserVisibleNeverVisible(t *testing.T) {
	calls := 0
	wantErr := errors.New("user: unknown user testuser")
	withStubUserExists(t, func(string) (bool, error) {
		calls++
		return false, wantErr
	})
	withFastPolling(t, 4)

	err := waitForUserVisible(context.Background(), "testuser")
	if err == nil {
		t.Fatal("waitForUserVisible() = nil, want error")
	}
	if calls != 4 {
		t.Errorf("userExistsFunc called %d times, want 4 (userVisiblePollAttempts)", calls)
	}
}

func TestWaitForUserVisibleContextCancelled(t *testing.T) {
	withStubUserExists(t, func(string) (bool, error) {
		return false, errors.New("not yet")
	})
	withFastPolling(t, 5)
	// Use a real (tiny) interval so the context has something to race
	// against, otherwise the immediate Done() would be indistinguishable
	// from a slow stub.
	userVisiblePollInterval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := waitForUserVisible(ctx, "testuser")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForUserVisible() = %v, want context.Canceled", err)
	}
}

const testGoogleComment = "# Added by Google"

func TestParseUserKeys(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     []string
	}{
		{
			name:     "empty",
			contents: "",
			want:     nil,
		},
		{
			name:     "manual_keys_only",
			contents: "ssh-ed25519 AAAA manual1\n# user comment\nssh-rsa BBBB manual2\n",
			want:     []string{"ssh-ed25519 AAAA manual1", "# user comment", "ssh-rsa BBBB manual2"},
		},
		{
			name:     "google_keys_only",
			contents: "# Added by Google\nssh-ed25519 AAAA google1\n# Added by Google\nssh-ed25519 BBBB google2\n",
			want:     nil,
		},
		{
			name:     "mixed_manual_and_google_keys",
			contents: "ssh-ed25519 AAAA manual1\n# Added by Google\nssh-ed25519 BBBB google1\nssh-rsa CCCC manual2\n",
			want:     []string{"ssh-ed25519 AAAA manual1", "ssh-rsa CCCC manual2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseUserKeys([]byte(tc.contents), testGoogleComment)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("parseUserKeys(%q) returned diff (-want +got):\n%s", tc.contents, diff)
			}
		})
	}
}

func TestIsAllowedAuthorizedKeysOwner(t *testing.T) {
	tests := []struct {
		name     string
		fileUID  uint32
		fileMode uint32
		wantUID  int
		want     bool
	}{
		{
			name:     "user_owned_0600",
			fileUID:  1001,
			fileMode: 0600,
			wantUID:  1001,
			want:     true,
		},
		{
			name:     "user_owned_0644",
			fileUID:  1001,
			fileMode: 0644,
			wantUID:  1001,
			want:     true,
		},
		{
			name:     "root_owned_world_readable_0644",
			fileUID:  0,
			fileMode: 0644,
			wantUID:  1001,
			want:     true,
		},
		{
			name:     "root_owned_secret_0600_rejected",
			fileUID:  0,
			fileMode: 0600,
			wantUID:  1001,
			want:     false,
		},
		{
			name:     "root_owned_group_readable_0640_rejected",
			fileUID:  0,
			fileMode: 0640,
			wantUID:  1001,
			want:     false,
		},
		{
			name:     "root_owned_0600_for_root_user",
			fileUID:  0,
			fileMode: 0600,
			wantUID:  0,
			want:     true,
		},
		{
			name:     "other_user_owned_0644_rejected",
			fileUID:  1002,
			fileMode: 0644,
			wantUID:  1001,
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isAllowedAuthorizedKeysOwner(tc.fileUID, tc.fileMode, tc.wantUID)
			if got != tc.want {
				t.Errorf("isAllowedAuthorizedKeysOwner(%d, %#o, %d) = %t, want %t", tc.fileUID, tc.fileMode, tc.wantUID, got, tc.want)
			}
		})
	}
}

func TestWriteAuthorizedKeysFileCreate(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "alice")
	if err := os.Mkdir(homeDir, 0750); err != nil {
		t.Fatalf("os.Mkdir(%q) = %v", homeDir, err)
	}
	pw := &passwdEntry{Username: "alice", UID: -1, GID: -1, HomeDir: homeDir}

	updated, err := writeAuthorizedKeysFile(pw, []string{"ssh-ed25519 AAAA key1"}, testGoogleComment)
	if err != nil {
		t.Fatalf("writeAuthorizedKeysFile() = %v, want nil", err)
	}
	if !updated {
		t.Errorf("writeAuthorizedKeysFile() updated = false, want true")
	}

	sshDir := filepath.Join(homeDir, ".ssh")
	sshInfo, err := os.Lstat(sshDir)
	if err != nil {
		t.Fatalf("os.Lstat(%q) = %v", sshDir, err)
	}
	if got := sshInfo.Mode().Perm(); got != 0700 {
		t.Errorf(".ssh mode = %#o, want 0700", got)
	}

	akPath := filepath.Join(sshDir, "authorized_keys")
	akInfo, err := os.Lstat(akPath)
	if err != nil {
		t.Fatalf("os.Lstat(%q) = %v", akPath, err)
	}
	if got := akInfo.Mode().Perm(); got != 0600 {
		t.Errorf("authorized_keys mode = %#o, want 0600", got)
	}
	gotBytes, err := os.ReadFile(akPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) = %v", akPath, err)
	}
	want := "# Added by Google\nssh-ed25519 AAAA key1\n"
	if string(gotBytes) != want {
		t.Errorf("authorized_keys = %q, want %q", string(gotBytes), want)
	}
}

func TestWriteAuthorizedKeysFilePreservesUserKeys(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "alice")
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("os.MkdirAll(%q) = %v", sshDir, err)
	}
	akPath := filepath.Join(sshDir, "authorized_keys")
	initial := "ssh-rsa MANUAL1 user@host\n# Added by Google\nssh-ed25519 OLDLONGKEY111111111111111111111111111111\n# Added by Google\nssh-ed25519 OLDLONGKEY222222222222222222222222222222\n"
	if err := os.WriteFile(akPath, []byte(initial), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", akPath, err)
	}
	pw := &passwdEntry{Username: "alice", UID: -1, GID: -1, HomeDir: homeDir}

	updated, err := writeAuthorizedKeysFile(pw, []string{"ssh-ed25519 SHORT"}, testGoogleComment)
	if err != nil {
		t.Fatalf("writeAuthorizedKeysFile() = %v, want nil", err)
	}
	if !updated {
		t.Errorf("writeAuthorizedKeysFile() updated = false, want true")
	}

	gotBytes, err := os.ReadFile(akPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) = %v", akPath, err)
	}
	want := "ssh-rsa MANUAL1 user@host\n# Added by Google\nssh-ed25519 SHORT\n"
	if string(gotBytes) != want {
		t.Errorf("authorized_keys = %q, want %q", string(gotBytes), want)
	}
}

func TestWriteAuthorizedKeysFileEmptyKeysMissingSSHDir(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "alice")
	if err := os.Mkdir(homeDir, 0750); err != nil {
		t.Fatalf("os.Mkdir(%q) = %v", homeDir, err)
	}

	pw := &passwdEntry{Username: "alice", UID: -1, GID: -1, HomeDir: homeDir}
	updated, err := writeAuthorizedKeysFile(pw, nil, testGoogleComment)
	if err != nil {
		t.Fatalf("writeAuthorizedKeysFile(%q, nil) = %v, want nil", homeDir, err)
	}
	if updated {
		t.Errorf("writeAuthorizedKeysFile(%q, nil) updated = true, want false", homeDir)
	}
	sshDir := filepath.Join(homeDir, ".ssh")
	if _, err := os.Lstat(sshDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("os.Lstat(%q) err = %v, want os.ErrNotExist", sshDir, err)
	}
}

func TestWriteAuthorizedKeysFileEmptyKeysMissingFile(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "alice")
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("os.MkdirAll(%q) = %v", sshDir, err)
	}

	pw := &passwdEntry{Username: "alice", UID: -1, GID: -1, HomeDir: homeDir}
	updated, err := writeAuthorizedKeysFile(pw, nil, testGoogleComment)
	if err != nil {
		t.Fatalf("writeAuthorizedKeysFile(%q, nil) = %v, want nil", homeDir, err)
	}
	if updated {
		t.Errorf("writeAuthorizedKeysFile(%q, nil) updated = true, want false", homeDir)
	}
	akPath := filepath.Join(sshDir, "authorized_keys")
	if _, err := os.Lstat(akPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("os.Lstat(%q) err = %v, want os.ErrNotExist", akPath, err)
	}
}

func TestWriteAuthorizedKeysFileEmptyKeysUnlinksSymlinkFile(t *testing.T) {
	base := t.TempDir()
	targetPath := filepath.Join(base, "target_keys")
	const targetContent = "# Added by Google\nssh-ed25519 AAAA revoked\n"
	if err := os.WriteFile(targetPath, []byte(targetContent), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", targetPath, err)
	}
	homeDir := filepath.Join(base, "alice")
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("os.MkdirAll(%q) = %v", sshDir, err)
	}
	akPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.Symlink(targetPath, akPath); err != nil {
		t.Fatalf("os.Symlink(%q, %q) = %v", targetPath, akPath, err)
	}

	pw := &passwdEntry{Username: "alice", UID: -1, GID: -1, HomeDir: homeDir}
	updated, err := writeAuthorizedKeysFile(pw, nil, testGoogleComment)
	if err != nil {
		t.Fatalf("writeAuthorizedKeysFile(%q, nil) = %v, want nil", homeDir, err)
	}
	if updated {
		t.Errorf("writeAuthorizedKeysFile(%q, nil) updated = true, want false", homeDir)
	}
	if _, err := os.Lstat(akPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("os.Lstat(%q) err = %v, want symlink removed", akPath, err)
	}
	gotTarget, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) = %v", targetPath, err)
	}
	if string(gotTarget) != targetContent {
		t.Errorf("target file = %q, want %q", string(gotTarget), targetContent)
	}
}

func TestWriteAuthorizedKeysFileEmptyKeysStripsGoogleKeys(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "alice")
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("os.MkdirAll(%q) = %v", sshDir, err)
	}
	akPath := filepath.Join(sshDir, "authorized_keys")
	existing := "ssh-rsa MANUAL user@host\n# Added by Google\nssh-ed25519 GOOGLE\n"
	if err := os.WriteFile(akPath, []byte(existing), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", akPath, err)
	}

	pw := &passwdEntry{Username: "alice", UID: -1, GID: -1, HomeDir: homeDir}
	updated, err := writeAuthorizedKeysFile(pw, nil, testGoogleComment)
	if err != nil {
		t.Fatalf("writeAuthorizedKeysFile(%q, nil) = %v, want nil", homeDir, err)
	}
	if !updated {
		t.Errorf("writeAuthorizedKeysFile(%q, nil) updated = false, want true", homeDir)
	}
	gotBytes, err := os.ReadFile(akPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) = %v", akPath, err)
	}
	const want = "ssh-rsa MANUAL user@host\n"
	if string(gotBytes) != want {
		t.Errorf("authorized_keys = %q, want %q", string(gotBytes), want)
	}
}

func TestWriteAuthorizedKeysFileRejectsSymlinkFile(t *testing.T) {
	base := t.TempDir()
	secretPath := filepath.Join(base, "target_file")
	const secretContent = "existing-target-content\n"
	if err := os.WriteFile(secretPath, []byte(secretContent), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", secretPath, err)
	}

	homeDir := filepath.Join(base, "alice")
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("os.MkdirAll(%q) = %v", sshDir, err)
	}
	akPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.Symlink(secretPath, akPath); err != nil {
		t.Fatalf("os.Symlink(%q, %q) = %v", secretPath, akPath, err)
	}

	pw := &passwdEntry{Username: "alice", UID: -1, GID: -1, HomeDir: homeDir}
	if _, err := writeAuthorizedKeysFile(pw, []string{"ssh-ed25519 AAAA key1"}, testGoogleComment); err == nil {
		t.Fatalf("writeAuthorizedKeysFile() = nil, want error for symlinked authorized_keys")
	}

	info, err := os.Lstat(akPath)
	if err != nil {
		t.Fatalf("os.Lstat(%q) = %v", akPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("authorized_keys was replaced (mode %v), want original symlink untouched", info.Mode())
	}
	gotSecret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) = %v", secretPath, err)
	}
	if string(gotSecret) != secretContent {
		t.Errorf("target file = %q, want %q", string(gotSecret), secretContent)
	}
}

func TestWriteAuthorizedKeysFileRejectsSymlinkSSHDir(t *testing.T) {
	base := t.TempDir()
	victimDir := filepath.Join(base, "victim")
	if err := os.Mkdir(victimDir, 0755); err != nil {
		t.Fatalf("os.Mkdir(%q) = %v", victimDir, err)
	}

	homeDir := filepath.Join(base, "alice")
	if err := os.Mkdir(homeDir, 0750); err != nil {
		t.Fatalf("os.Mkdir(%q) = %v", homeDir, err)
	}
	if err := os.Symlink(victimDir, filepath.Join(homeDir, ".ssh")); err != nil {
		t.Fatalf("os.Symlink() = %v", err)
	}

	pw := &passwdEntry{Username: "alice", UID: -1, GID: -1, HomeDir: homeDir}
	if _, err := writeAuthorizedKeysFile(pw, []string{"ssh-ed25519 AAAA key1"}, testGoogleComment); err == nil {
		t.Fatalf("writeAuthorizedKeysFile() = nil, want error for symlinked .ssh")
	}
	if _, err := os.Lstat(filepath.Join(victimDir, "authorized_keys")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("victim authorized_keys Lstat err = %v, want os.ErrNotExist", err)
	}
}

func TestWriteAuthorizedKeysFileRejectsSymlinkHomeDir(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	if err := os.Mkdir(realDir, 0750); err != nil {
		t.Fatalf("os.Mkdir(%q) = %v", realDir, err)
	}
	symlinkHome := filepath.Join(base, "alice_link")
	if err := os.Symlink(realDir, symlinkHome); err != nil {
		t.Fatalf("os.Symlink() = %v", err)
	}

	pw := &passwdEntry{Username: "alice", UID: -1, GID: -1, HomeDir: symlinkHome}
	if _, err := writeAuthorizedKeysFile(pw, []string{"ssh-ed25519 AAAA key1"}, testGoogleComment); err == nil {
		t.Fatalf("writeAuthorizedKeysFile() = nil, want error for symlinked HomeDir")
	}
	if _, err := os.Lstat(filepath.Join(realDir, ".ssh")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("os.Lstat(%q) err = %v, want os.ErrNotExist", filepath.Join(realDir, ".ssh"), err)
	}
}

func TestWriteAuthorizedKeysFileRejectsFIFO(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "alice")
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("os.MkdirAll(%q) = %v", sshDir, err)
	}
	fifoPath := filepath.Join(sshDir, "authorized_keys")
	if err := unix.Mkfifo(fifoPath, 0600); err != nil {
		t.Fatalf("unix.Mkfifo(%q) = %v", fifoPath, err)
	}

	pw := &passwdEntry{Username: "alice", UID: -1, GID: -1, HomeDir: homeDir}
	if _, err := writeAuthorizedKeysFile(pw, []string{"ssh-ed25519 AAAA key1"}, testGoogleComment); err == nil {
		t.Fatalf("writeAuthorizedKeysFile() = nil, want error for FIFO authorized_keys")
	}
	info, err := os.Lstat(fifoPath)
	if err != nil {
		t.Fatalf("os.Lstat(%q) = %v", fifoPath, err)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("os.Lstat(%q) mode = %v, want named pipe untouched", fifoPath, info.Mode())
	}
}

func TestWriteAuthorizedKeysFileRejectsHardlink(t *testing.T) {
	base := t.TempDir()
	secretPath := filepath.Join(base, "linked_file")
	const wantContent = "existing-link-content\n"
	if err := os.WriteFile(secretPath, []byte(wantContent), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", secretPath, err)
	}

	homeDir := filepath.Join(base, "alice")
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("os.MkdirAll(%q) = %v", sshDir, err)
	}
	akPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.Link(secretPath, akPath); err != nil {
		t.Fatalf("os.Link(%q, %q) = %v", secretPath, akPath, err)
	}

	pw := &passwdEntry{Username: "alice", UID: -1, GID: -1, HomeDir: homeDir}
	if _, err := writeAuthorizedKeysFile(pw, []string{"ssh-ed25519 AAAA key1"}, testGoogleComment); err == nil {
		t.Fatalf("writeAuthorizedKeysFile() = nil, want error for hardlinked authorized_keys")
	}
	srcInfo, err := os.Lstat(secretPath)
	if err != nil {
		t.Fatalf("os.Lstat(%q) = %v", secretPath, err)
	}
	akInfo, err := os.Lstat(akPath)
	if err != nil {
		t.Fatalf("os.Lstat(%q) = %v", akPath, err)
	}
	if !os.SameFile(srcInfo, akInfo) {
		t.Errorf("os.SameFile(%q, %q) = false, want hardlink untouched", secretPath, akPath)
	}
	gotSecret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) = %v", secretPath, err)
	}
	if string(gotSecret) != wantContent {
		t.Errorf("linked file = %q, want %q", string(gotSecret), wantContent)
	}
}

func TestReadAuthorizedKeysAtRejectsDisallowedOwner(t *testing.T) {
	sshDir := t.TempDir()
	akPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(akPath, []byte("ssh-ed25519 AAAA key1\n"), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", akPath, err)
	}

	sshFD, err := unix.Open(sshDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("unix.Open(%q) = %v", sshDir, err)
	}
	defer unix.Close(sshFD)

	otherUID := os.Getuid() + 1
	if _, _, err := readAuthorizedKeysAt(sshFD, otherUID, true); err == nil {
		t.Fatalf("readAuthorizedKeysAt(sshFD, %d, true) = nil, want error", otherUID)
	}
}

func TestReadAuthorizedKeysAtAllowsMatchingOwner(t *testing.T) {
	sshDir := t.TempDir()
	akPath := filepath.Join(sshDir, "authorized_keys")
	const wantContent = "ssh-ed25519 AAAA key1\n"
	if err := os.WriteFile(akPath, []byte(wantContent), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", akPath, err)
	}

	sshFD, err := unix.Open(sshDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("unix.Open(%q) = %v", sshDir, err)
	}
	defer unix.Close(sshFD)

	selfUID := os.Getuid()
	gotBytes, exists, err := readAuthorizedKeysAt(sshFD, selfUID, true)
	if err != nil {
		t.Fatalf("readAuthorizedKeysAt(sshFD, %d, true) = %v, want nil", selfUID, err)
	}
	if !exists {
		t.Errorf("readAuthorizedKeysAt(sshFD, %d, true) exists = false, want true", selfUID)
	}
	if string(gotBytes) != wantContent {
		t.Errorf("readAuthorizedKeysAt(sshFD, %d, true) = %q, want %q", selfUID, string(gotBytes), wantContent)
	}
}

func TestReadAuthorizedKeysAtRejectsOversizedFile(t *testing.T) {
	sshDir := t.TempDir()
	akPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(akPath, []byte("ssh-ed25519 AAAA key1\n"), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", akPath, err)
	}
	if err := os.Truncate(akPath, maxAuthorizedKeysBytes+1); err != nil {
		t.Fatalf("os.Truncate(%q) = %v", akPath, err)
	}

	sshFD, err := unix.Open(sshDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("unix.Open(%q) = %v", sshDir, err)
	}
	defer unix.Close(sshFD)

	if _, _, err := readAuthorizedKeysAt(sshFD, os.Getuid(), true); err == nil {
		t.Fatalf("readAuthorizedKeysAt() = nil, want error for file exceeding maxAuthorizedKeysBytes")
	}
}

func TestCheckTrustedDirStat(t *testing.T) {
	tests := []struct {
		name        string
		st          unix.Stat_t
		wantUID     int
		enforceRoot bool
		wantErr     bool
	}{
		{
			name:        "non_directory_rejected",
			st:          unix.Stat_t{Mode: unix.S_IFREG | 0700, Uid: 1001},
			wantUID:     1001,
			enforceRoot: true,
			wantErr:     true,
		},
		{
			name:        "user_owned_0750_allowed",
			st:          unix.Stat_t{Mode: unix.S_IFDIR | 0750, Uid: 1001, Gid: 1001},
			wantUID:     1001,
			enforceRoot: true,
			wantErr:     false,
		},
		{
			name:        "user_owned_group_writable_upg_allowed",
			st:          unix.Stat_t{Mode: unix.S_IFDIR | 0775, Uid: 1001, Gid: 1001},
			wantUID:     1001,
			enforceRoot: true,
			wantErr:     false,
		},
		{
			name:        "world_writable_rejected_even_with_sticky",
			st:          unix.Stat_t{Mode: unix.S_IFDIR | unix.S_ISVTX | 0777, Uid: 1001},
			wantUID:     1001,
			enforceRoot: true,
			wantErr:     true,
		},
		{
			name:        "root_owned_0755_allowed",
			st:          unix.Stat_t{Mode: unix.S_IFDIR | 0755, Uid: 0, Gid: 0},
			wantUID:     1001,
			enforceRoot: true,
			wantErr:     false,
		},
		{
			name:        "root_owned_root_group_writable_allowed",
			st:          unix.Stat_t{Mode: unix.S_IFDIR | 0775, Uid: 0, Gid: 0},
			wantUID:     1001,
			enforceRoot: true,
			wantErr:     false,
		},
		{
			name:        "root_user_root_owned_group_writable_allowed",
			st:          unix.Stat_t{Mode: unix.S_IFDIR | 0775, Uid: 0, Gid: 100},
			wantUID:     0,
			enforceRoot: true,
			wantErr:     false,
		},
		{
			name:        "root_owned_non_root_group_writable_rejected",
			st:          unix.Stat_t{Mode: unix.S_IFDIR | 0775, Uid: 0, Gid: 100},
			wantUID:     1001,
			enforceRoot: true,
			wantErr:     true,
		},
		{
			name:        "foreign_uid_rejected",
			st:          unix.Stat_t{Mode: unix.S_IFDIR | 0700, Uid: 1002, Gid: 1002},
			wantUID:     1001,
			enforceRoot: true,
			wantErr:     true,
		},
		{
			name:        "negative_uid_skips_owner_check",
			st:          unix.Stat_t{Mode: unix.S_IFDIR | 0700, Uid: 1002, Gid: 1002},
			wantUID:     -1,
			enforceRoot: true,
			wantErr:     false,
		},
		{
			name:        "not_enforced_foreign_uid_allowed",
			st:          unix.Stat_t{Mode: unix.S_IFDIR | 0777, Uid: 1002, Gid: 1002},
			wantUID:     1001,
			enforceRoot: false,
			wantErr:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkTrustedDirStat(&tc.st, tc.wantUID, tc.enforceRoot)
			if (err != nil) != tc.wantErr {
				t.Errorf("checkTrustedDirStat(%+v, %d, %t) err = %v, wantErr %t", tc.st, tc.wantUID, tc.enforceRoot, err, tc.wantErr)
			}
		})
	}
}

func TestOpenHomeDirAllowsAncestorSymlink(t *testing.T) {
	base := t.TempDir()
	realHomeRoot := filepath.Join(base, "usr_home")
	realAlice := filepath.Join(realHomeRoot, "alice")
	if err := os.MkdirAll(realAlice, 0750); err != nil {
		t.Fatalf("os.MkdirAll(%q) = %v", realAlice, err)
	}
	homeLink := filepath.Join(base, "home")
	if err := os.Symlink(realHomeRoot, homeLink); err != nil {
		t.Fatalf("os.Symlink(%q, %q) = %v", realHomeRoot, homeLink, err)
	}

	fd, err := openHomeDir(filepath.Join(homeLink, "alice"), -1, false)
	if err != nil {
		t.Fatalf("openHomeDir() = %v, want nil", err)
	}
	_ = unix.Close(fd)
}

func TestRemoveGoogleUserRevokesSudoOnKeyError(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "removed.txt")
	script := filepath.Join(dir, "fake_gpasswd.sh")
	scriptBody := "#!/bin/sh\nprintf '%s %s' \"$1\" \"$2\" > " + marker + "\n"
	if err := os.WriteFile(script, []byte(scriptBody), 0755); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", script, err)
	}

	config := &cfg.Sections{
		Accounts: &cfg.Accounts{
			DeprovisionRemove: false,
			GPasswdRemoveCmd:  script + " {user} {group}",
		},
	}
	const missingUser = "nonexistent_test_user_999999"
	if err := removeGoogleUser(t.Context(), config, missingUser); err == nil {
		t.Fatalf("removeGoogleUser(%q) = nil, want error from missing passwd entry", missingUser)
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("GPasswdRemoveCmd did not run (%q): %v", marker, err)
	}
	const want = missingUser + " google-sudoers"
	if string(got) != want {
		t.Errorf("GPasswdRemoveCmd args = %q, want %q", string(got), want)
	}
}
