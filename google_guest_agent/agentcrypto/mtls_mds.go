//  Copyright 2023 Google Inc. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

// Package agentcrypto provides various cryptography related utility functions for Guest Agent.
package agentcrypto

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/uefi"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/google/go-tpm-tools/client"
	"github.com/google/go-tpm-tools/proto/tpm"
	"github.com/google/go-tpm/legacy/tpm2"
	"google.golang.org/protobuf/encoding/protojson"

	pb "github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/agentcrypto/credentials"
)

const (
	// UEFI variables are of format {VariableName}-{VendorGUID}
	// googleGUID is Google's (vendors/variable owners) GUID used to prevent name collision with other vendors.
	googleGUID = "8be4df61-93ca-11d2-aa0d-00e098032b8c"
	// googleRootCACertEFIVarName is predefined string part of the UEFI variable name that holds Root CA cert.
	googleRootCACertEFIVarName = "InstanceRootCACertificate"
	// rootCACertFileName is the root CA cert.
	rootCACertFileName = "root.crt"
	// clientCredsFileName are client credentials, its basically the file
	// that has the EC private key and the client certificate concatenated.
	clientCredsFileName = "client.key"
	// clientCertsKey is the metadata server key at which client identity certificate is exposed.
	clientCertsKey = "instance/credentials/certs"
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

	if _, err := parseCertificate(rootCACert.Content); err != nil {
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

// getClientCredentials fetches encrypted credentials from MDS and unmarshal it into GuestCredentialsResponse.
func getClientCredentials(ctx context.Context, client metadata.MDSClientInterface) (*pb.GuestCredentialsResponse, error) {
	creds, err := client.GetKey(ctx, clientCertsKey, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to get client credentials from MDS: %w", err)
	}

	res := &pb.GuestCredentialsResponse{}
	if err := protojson.Unmarshal([]byte(creds), res); err != nil {
		return nil, fmt.Errorf("unable to unmarshal MDS response(%+v): %w", creds, err)
	}

	return res, nil
}

// extractKey decrypts the key cipher text (Key encryption Key encrypted Data Dencryption Key)
// through vTPM and returns the key (DEK) as plain text.
func extractKey(importBlob *tpm.ImportBlob) ([]byte, error) {
	rwc, err := tpm2.OpenTPM()
	if err != nil {
		return nil, fmt.Errorf("unable to open a channel to the TPM: %w", err)
	}
	defer rwc.Close()

	ek, err := client.EndorsementKeyECC(rwc)
	if err != nil {
		return nil, fmt.Errorf("failed to load a key from TPM: %w", err)
	}
	defer ek.Close()

	dek, err := ek.Import(importBlob)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt import blob: %w", err)
	}

	return dek, nil
}

// fetchAndWriteClientCredentials fetches encrypted client credentials from MDS,
// extracts Key Encryption Key (KEK) from vTPM, decrypts the client credentials using KEK,
// verifies these credentials are signed by root CA and writes it to the output file.
func fetchAndWriteClientCredentials(ctx context.Context, rootCA, outputFile string) error {
	resp, err := getClientCredentials(ctx, metadata.New())
	if err != nil {
		return err
	}

	dek, err := extractKey(resp.GetKeyImportBlob())
	if err != nil {
		return err
	}

	plaintext, err := decrypt(dek, resp.GetEncryptedCredentials(), nil)
	if err != nil {
		return err
	}

	if err := verifySign(plaintext, rootCA); err != nil {
		return err
	}

	if err := os.WriteFile(outputFile, plaintext, 0644); err != nil {
		return fmt.Errorf("unable to write client credentials to file: %w", err)
	}

	logger.Infof("Successfully wrote client credentials to %q", outputFile)
	return nil
}

// Bootstrap generates the required credentials for MTLS MDS workflow.
//
// 1. Fetches, verifies and writes Root CA cert from UEFI variable to /etc/pki/tls/certs/mds/root.crt
// 2. Fetches encrypted client credentials from MDS, decrypts it via vTPM and writes it to /etc/pki/tls/certs/mds/client.key
//
// Example usage of these credentials to call HTTPS endpoint of MDS:
//
// curl --cacert /etc/pki/tls/certs/mds/root.crt -E /etc/pki/tls/certs/mds/client.key -H "MetadataFlavor: Google" https://169.254.169.254
func Bootstrap(ctx context.Context) error {
	// defaultCredsDir is the directory location for MTLS MDS credentials.
	defaultCredsDir := "/etc/pki/tls/certs/mds"

	// TODO: Finalize on where to store certificates on windows.
	if runtime.GOOS == "windows" {
		defaultCredsDir = `C:\Users`
	}

	logger.Infof("Fetching Root CA cert...")
	if err := readAndWriteRootCACert(googleRootCACertUEFIVar, filepath.Join(defaultCredsDir, rootCACertFileName)); err != nil {
		return fmt.Errorf("failed to read Root CA cert with an error: %w", err)
	}

	logger.Infof("Fetching client credentials...")
	if err := fetchAndWriteClientCredentials(ctx, filepath.Join(defaultCredsDir, rootCACertFileName), filepath.Join(defaultCredsDir, clientCredsFileName)); err != nil {
		return fmt.Errorf("failed to generate client credentials with an error: %w", err)
	}

	return nil
}
