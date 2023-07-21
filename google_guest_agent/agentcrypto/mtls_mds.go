// Package agentcrypto provides various cryptography related utility functions for Guest Agent.
package agentcrypto

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/uefi"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	googleGUID                 = "8be4df61-93ca-11d2-aa0d-00e098032b8c"
	googleRootCACertEFIVarName = "InstanceRootCACertificate"
	defaultCredsDir            = "/etc/pki/tls/certs/mds"
	rootCACertFileName         = "root.crt"
)

var (
	googleRootCACertUEFIVar = uefi.VariableName{Name: googleRootCACertEFIVarName, GUID: googleGUID}
)

// readAndWriteRootCACert reads Root CA cert from UEFI variable and writes it to output file.
func readAndWriteRootCACert(name uefi.VariableName, outputFile string) error {
	rootCACert, err := uefi.ReadVariable(name)

	if err != nil {
		return fmt.Errorf("unable to read root CA cert file contents: %w", err)
	}

	if err := VerifyCertificate(rootCACert.Content); err != nil {
		return fmt.Errorf("unable to verify Root CA cert: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputFile), 0644); err != nil {
		return fmt.Errorf("unable to create required directories for %q: %w", outputFile, err)
	}

	if err := os.WriteFile(outputFile, rootCACert.Content, 0644); err != nil {
		return fmt.Errorf("unable to write root CA cert file contents to file: %w", err)
	}

	logger.Infof("Successfully wrote root CA Cert file to %q", outputFile)
	return nil
}

// Bootstrap generates the required credentials for MTLS MDS workflow.
// 1. Fetches, verifies and writes Root CA cert from UEFI variable to /etc/pki/tls/certs/mds/root.crt
// 2. Fetches encrypted client credentials from MDS, decrypts it via vTPM and writes it to /etc/pki/tls/certs/mds/client.key (TODO)
func Bootstrap() error {
	logger.Infof("Fetching Root CA cert...")

	if err := readAndWriteRootCACert(googleRootCACertUEFIVar, filepath.Join(defaultCredsDir, rootCACertFileName)); err != nil {
		return fmt.Errorf("failed to read Root CA cert with an error: %w", err)
	}

	return nil
}
