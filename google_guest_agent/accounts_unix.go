// Copyright 2019 Google LLC

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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"syscall"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"golang.org/x/sys/unix"
)

func getUIDAndGID(path string) (string, string) {
	if dir, err := os.Stat(path); err == nil {
		if stat, ok := dir.Sys().(*syscall.Stat_t); ok {
			return fmt.Sprintf("%d", stat.Uid), fmt.Sprintf("%d", stat.Gid)
		}
	}
	return "", ""
}

func createUser(ctx context.Context, username, uid, gid string) error {
	config := cfg.Get()
	useradd := config.Accounts.UserAddCmd
	if uid != "" {
		useradd = fmt.Sprintf("%s -u %s", useradd, uid)
	}
	if gid != "" {
		groupadd := config.Accounts.GroupAddCmd
		groupadd = fmt.Sprintf("%s -g %s", groupadd, gid)
		cmd, args := createUserGroupCmd(groupadd, "", username)
		if err := run.Quiet(ctx, cmd, args...); err != nil {
			return err
		}
		useradd = fmt.Sprintf("%s -g %s", useradd, gid)
	}
	cmd, args := createUserGroupCmd(useradd, username, "")
	return run.Quiet(ctx, cmd, args...)
}

func addUserToGroup(ctx context.Context, user, group string) error {
	config := cfg.Get()
	gpasswdadd := config.Accounts.GPasswdAddCmd
	cmd, args := createUserGroupCmd(gpasswdadd, user, group)
	return run.Quiet(ctx, cmd, args...)
}

func userExists(name string) (bool, error) {
	if _, err := user.Lookup(name); err != nil {
		return false, err
	}
	return true, nil
}

const maxAuthorizedKeysBytes = 10 << 20 // 10 MiB

// isAllowedAuthorizedKeysOwner reports whether an existing authorized_keys file
// with fileUID and fileMode may be read when reconciling keys for wantUID. It
// accepts files owned by wantUID as well as world-readable root-owned files
// (matching OpenSSH StrictModes while blocking non-public root files).
func isAllowedAuthorizedKeysOwner(fileUID, fileMode uint32, wantUID int) bool {
	if wantUID >= 0 && fileUID == uint32(wantUID) {
		return true
	}
	return fileUID == 0 && fileMode&0o004 != 0
}

func openAtNoFollow(dirFD int, name string, flags int, mode uint32) (int, error) {
	openFlags := flags | unix.O_NOFOLLOW | unix.O_CLOEXEC
	for {
		fd, err := unix.Openat(dirFD, name, openFlags, mode)
		if !errors.Is(err, unix.EINTR) {
			return fd, err
		}
	}
}

// checkTrustedDirStat validates HomeDir or .ssh. When enforceRoot is true, the
// directory must not be world-writable, must be owned by wantUID or root, and
// if root-owned for a non-root user must not be group-writable by a non-root
// group.
func checkTrustedDirStat(st *unix.Stat_t, wantUID int, enforceRoot bool) error {
	if uint32(st.Mode)&uint32(unix.S_IFMT) != uint32(unix.S_IFDIR) {
		return fmt.Errorf("not a directory (mode %#o)", uint32(st.Mode))
	}
	if !enforceRoot {
		return nil
	}
	if uint32(st.Mode)&0o002 != 0 {
		return fmt.Errorf("directory is world-writable (mode %#o)", uint32(st.Mode)&0o777)
	}
	if wantUID >= 0 {
		if st.Uid != uint32(wantUID) && st.Uid != 0 {
			return fmt.Errorf("directory has unexpected owner uid %d, want %d or 0", st.Uid, wantUID)
		}
		if st.Uid == 0 && wantUID != 0 && st.Gid != 0 && uint32(st.Mode)&0o020 != 0 {
			return fmt.Errorf("root-owned directory with non-root gid %d is group-writable (mode %#o)", st.Gid, uint32(st.Mode)&0o777)
		}
	}
	return nil
}

// openHomeDir opens homeDir (configured in root-managed /etc/passwd) without
// following a symlink on its final path component and verifies ownership and
// permissions via checkTrustedDirStat. The caller must close the returned fd.
func openHomeDir(homeDir string, uid int, enforceRoot bool) (int, error) {
	if !filepath.IsAbs(homeDir) {
		return -1, fmt.Errorf("homedir %q must be an absolute path", homeDir)
	}
	cleanHome := filepath.Clean(homeDir)
	if cleanHome == "/" {
		return -1, fmt.Errorf("refusing %q as user homedir", homeDir)
	}
	homeFD, err := openAtNoFollow(unix.AT_FDCWD, cleanHome, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return -1, fmt.Errorf("opening homedir %q: %w", homeDir, err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(homeFD, &st); err != nil {
		_ = unix.Close(homeFD)
		return -1, err
	}
	if err := checkTrustedDirStat(&st, uid, enforceRoot); err != nil {
		_ = unix.Close(homeFD)
		return -1, fmt.Errorf("homedir %q: %w", homeDir, err)
	}
	return homeFD, nil
}

// openOrCreateSSHDir opens ".ssh" inside homeFD without following symlinks,
// creating it with mode 0700 and uid:gid ownership when createIfMissing is
// true. The caller must close the returned fd.
func openOrCreateSSHDir(homeFD, uid, gid int, createIfMissing, enforceRoot bool) (int, error) {
	fd, err := openAtNoFollow(homeFD, ".ssh", unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		if !errors.Is(err, unix.ENOENT) || !createIfMissing {
			return -1, fmt.Errorf("opening .ssh directory: %w", err)
		}
		if err := unix.Mkdirat(homeFD, ".ssh", 0700); err != nil {
			return -1, fmt.Errorf("creating .ssh directory: %w", err)
		}
		fd, err = openAtNoFollow(homeFD, ".ssh", unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if err != nil {
			_ = unix.Unlinkat(homeFD, ".ssh", unix.AT_REMOVEDIR)
			return -1, fmt.Errorf("opening newly created .ssh directory: %w", err)
		}
		var createdSt unix.Stat_t
		if err := unix.Fstat(fd, &createdSt); err != nil {
			_ = unix.Close(fd)
			_ = unix.Unlinkat(homeFD, ".ssh", unix.AT_REMOVEDIR)
			return -1, err
		}
		if createdSt.Uid != uint32(os.Geteuid()) {
			_ = unix.Close(fd)
			return -1, fmt.Errorf(".ssh directory owner changed after creation (uid %d)", createdSt.Uid)
		}
		if uid >= 0 || gid >= 0 {
			if err := unix.Fchown(fd, uid, gid); err != nil {
				_ = unix.Close(fd)
				_ = unix.Unlinkat(homeFD, ".ssh", unix.AT_REMOVEDIR)
				return -1, fmt.Errorf("setting ownership of .ssh directory: %w", err)
			}
		}
		if err := unix.Fchmod(fd, 0700); err != nil {
			_ = unix.Close(fd)
			_ = unix.Unlinkat(homeFD, ".ssh", unix.AT_REMOVEDIR)
			return -1, fmt.Errorf("setting permissions on .ssh directory: %w", err)
		}
	}

	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if err := checkTrustedDirStat(&st, uid, enforceRoot); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf(".ssh directory: %w", err)
	}
	return fd, nil
}

// readAuthorizedKeysAt reads "authorized_keys" relative to sshFD without
// following symlinks or blocking on FIFOs, returning (nil, false, nil) if it
// does not exist. Ownership checks are enforced when enforceOwner is true.
func readAuthorizedKeysAt(sshFD, uid int, enforceOwner bool) (contents []byte, exists bool, err error) {
	fd, err := openAtNoFollow(sshFD, "authorized_keys", unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("opening authorized_keys: %w", err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return nil, false, err
	}
	if uint32(st.Mode)&uint32(unix.S_IFMT) != uint32(unix.S_IFREG) {
		_ = unix.Close(fd)
		return nil, false, fmt.Errorf("authorized_keys is not a regular file (mode %#o)", uint32(st.Mode))
	}
	if uint64(st.Nlink) != 1 {
		_ = unix.Close(fd)
		return nil, false, fmt.Errorf("authorized_keys has %d hard links, want 1", uint64(st.Nlink))
	}
	if enforceOwner && uid >= 0 && !isAllowedAuthorizedKeysOwner(st.Uid, uint32(st.Mode), uid) {
		_ = unix.Close(fd)
		return nil, false, fmt.Errorf("authorized_keys has disallowed owner uid %d (mode %#o) for user uid %d", st.Uid, uint32(st.Mode)&0o777, uid)
	}
	if st.Size > maxAuthorizedKeysBytes {
		_ = unix.Close(fd)
		return nil, false, fmt.Errorf("authorized_keys exceeds max size (%d > %d)", st.Size, maxAuthorizedKeysBytes)
	}

	_ = unix.SetNonblock(fd, false)
	f := os.NewFile(uintptr(fd), "authorized_keys")
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxAuthorizedKeysBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maxAuthorizedKeysBytes {
		return nil, false, fmt.Errorf("authorized_keys exceeds max size (%d bytes)", maxAuthorizedKeysBytes)
	}
	return data, true, nil
}

// writeAuthorizedKeysAt writes content to a unique O_EXCL temporary file inside
// sshFD, sets ownership and 0600 permissions on the open handle, syncs, and
// atomically renames it over "authorized_keys".
func writeAuthorizedKeysAt(sshFD int, content []byte, uid, gid int) error {
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return fmt.Errorf("generating temp suffix: %w", err)
	}
	tmpName := ".authorized_keys.google." + hex.EncodeToString(rnd[:])

	tmpFD, err := openAtNoFollow(sshFD, tmpName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("creating temporary authorized_keys: %w", err)
	}
	tmpFile := os.NewFile(uintptr(tmpFD), tmpName)
	cleanup := func() {
		_ = tmpFile.Close()
		_ = unix.Unlinkat(sshFD, tmpName, 0)
	}

	if _, err := tmpFile.Write(content); err != nil {
		cleanup()
		return fmt.Errorf("writing temporary authorized_keys: %w", err)
	}
	if uid >= 0 || gid >= 0 {
		if err := tmpFile.Chown(uid, gid); err != nil {
			cleanup()
			return fmt.Errorf("setting ownership of new keys file: %w", err)
		}
	}
	if err := tmpFile.Chmod(0600); err != nil {
		cleanup()
		return fmt.Errorf("setting permissions on new keys file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("syncing new keys file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = unix.Unlinkat(sshFD, tmpName, 0)
		return fmt.Errorf("closing new keys file: %w", err)
	}
	if err := unix.Renameat(sshFD, tmpName, sshFD, "authorized_keys"); err != nil {
		_ = unix.Unlinkat(sshFD, tmpName, 0)
		return fmt.Errorf("renaming temporary authorized_keys: %w", err)
	}
	return nil
}

// writeAuthorizedKeysFile reconciles keys into passwd.HomeDir/.ssh/authorized_keys
// using directory-pinned descriptors, returning true when authorized_keys was
// written. Ownership checks apply when running as root (euid == 0).
func writeAuthorizedKeysFile(passwd *passwdEntry, keys []string, gcomment string) (bool, error) {
	enforceRoot := os.Geteuid() == 0
	homeFD, err := openHomeDir(passwd.HomeDir, passwd.UID, enforceRoot)
	if err != nil {
		return false, err
	}
	defer unix.Close(homeFD)

	sshFD, err := openOrCreateSSHDir(homeFD, passwd.UID, passwd.GID, len(keys) > 0, enforceRoot)
	if err != nil {
		if errors.Is(err, unix.ENOENT) && len(keys) == 0 {
			return false, nil
		}
		return false, err
	}
	defer unix.Close(sshFD)

	akContents, exists, err := readAuthorizedKeysAt(sshFD, passwd.UID, enforceRoot)
	if err != nil {
		if len(keys) == 0 {
			if uerr := unix.Unlinkat(sshFD, "authorized_keys", 0); uerr != nil && !errors.Is(uerr, unix.ENOENT) {
				return false, fmt.Errorf("removing unreadable authorized_keys: %w", uerr)
			}
			return false, nil
		}
		return false, err
	}
	if !exists && len(keys) == 0 {
		return false, nil
	}

	userKeys := parseUserKeys(akContents, gcomment)
	var buf bytes.Buffer
	for _, key := range userKeys {
		fmt.Fprintf(&buf, "%s\n", key)
	}
	for _, key := range keys {
		fmt.Fprintf(&buf, "%s\n%s\n", gcomment, key)
	}

	if err := writeAuthorizedKeysAt(sshFD, buf.Bytes(), passwd.UID, passwd.GID); err != nil {
		return false, err
	}
	return true, nil
}
