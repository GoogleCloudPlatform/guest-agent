// Copyright 2023 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package agentcrypto

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	// defaultCredsDir is the directory location for MTLS MDS credentials.
	defaultCredsDir = "/run/google-mds-mtls"
	// rootCACertFileName is the root CA cert.
	rootCACertFileName = "root.crt"
	// clientCredsFileName are client credentials, its basically the file
	// that has the EC private key and the client certificate concatenated.
	clientCredsFileName = "client.key"
)

// writeRootCACert writes Root CA cert from UEFI variable to output file.
func (j *CredsJob) writeRootCACert(ctx context.Context, content []byte, outputFile string) error {
	if err := utils.SaferWriteFile(content, outputFile, 0644); err != nil {
		return err
	}

	// Best effort to update system store, don't fail.
	if err := updateSystemStore(ctx, outputFile); err != nil {
		logger.Errorf("Failed add Root MDS cert to system trust store with error: %v", err)
	}

	return nil
}

// writeClientCredentials stores client credentials (certificate and private key).
func (j *CredsJob) writeClientCredentials(plaintext []byte, outputFile string) error {
	return utils.SaferWriteFile(plaintext, outputFile, 0644)
}

// getCAStoreUpdater interates over known system trust store updaters and returns the first found.
func getCAStoreUpdater() (string, error) {
	knownUpdaters := []string{"update-ca-certificates", "update-ca-trust"}
	var errs []string

	for _, u := range knownUpdaters {
		_, err := exec.LookPath(u)
		if err == nil {
			return u, nil
		}
		errs = append(errs, err.Error())
	}

	return "", fmt.Errorf("no known trust updaters %v were found: %v", knownUpdaters, errs)
}

// certificateDirFromUpdater returns directory of local CA certificates for the given updater tool.
func certificateDirFromUpdater(updater string) (string, error) {
	switch updater {
	// SUSE, Debian and Ubuntu distributions.
	// https://manpages.ubuntu.com/manpages/xenial/man8/update-ca-certificates.8.html
	case "update-ca-certificates":
		return "/usr/local/share/ca-certificates/", nil
	// CentOS, Fedora, RedHat distributions.
	// https://www.unix.com/man-page/centos/8/UPDATE-CA-TRUST/
	case "update-ca-trust":
		return "/etc/pki/ca-trust/source/anchors/", nil
	default:
		return "", fmt.Errorf("unknown updater %q, no local trusted CA certificate directory found", updater)
	}
}

// updateSystemStore updates the local system store with the cert.
func updateSystemStore(ctx context.Context, cert string) error {
	cmd, err := getCAStoreUpdater()
	if err != nil {
		return err
	}

	dir, err := certificateDirFromUpdater(cmd)
	if err != nil {
		return err
	}

	dest := filepath.Join(dir, filepath.Base(cert))

	if err := utils.CopyFile(cert, dest, 0644); err != nil {
		return err
	}

	res := run.WithOutput(ctx, cmd)
	if res.ExitCode != 0 {
		return fmt.Errorf("command %q failed with error: %s", cmd, res.Error())
	}

	logger.Infof("Certificate %q added to system store successfully %s", cert, res.StdOut)
	return nil
}
