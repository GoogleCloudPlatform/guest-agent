//  Copyright 2022 Google LLC
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

// gce_workload_cert_refresh downloads and rotates workload certificates for GCE VMs.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	contentDirPrefix  = "/run/secrets/workload-spiffe-contents"
	tempSymlinkPrefix = "/run/secrets/workload-spiffe-symlink"
	symlink           = "/run/secrets/workload-spiffe-credentials"
)

var (
	programName    = "gce_workload_certs_refresh"
	version        string
	metadataURL    = "http://169.254.169.254/computeMetadata/v1/"
	defaultTimeout = 2 * time.Second
)

func logFormat(e logger.LogEntry) string {
	now := time.Now().Format("2006/01/02 15:04:05")
	return fmt.Sprintf("%s: %s", now, e.Message)
}

func getMetadata(key string) ([]byte, error) {
	client := &http.Client{
		Timeout: defaultTimeout,
	}

	url := metadataURL + key
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Metadata-Flavor", "Google")

	var res *http.Response

	// Retry up to 5 times
	for i := 1; i < 6; i++ {
		res, err = client.Do(req)
		if err == nil {
			break
		}
		logger.Errorf("error connecting to metadata server, retrying in 3s, error: %v", err)
		time.Sleep(time.Duration(3) * time.Second)
	}
	if err != nil {
		return nil, err
	}

	if res.StatusCode == 404 {
		return nil, fmt.Errorf("HTTP 404")
	}

	// GCE Workload Certificate endpoints return 412 Precondition failed if the VM was
	// never configured with valid config values at least once. Without valid config
	// values GCE cannot provision the workload certificates.
	if res.StatusCode == 412 {
		return nil, fmt.Errorf("HTTP 412")
	}

	defer res.Body.Close()
	md, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return md, nil
}

/*
metadata key instance/workload-identities

	{
	 "status": "OK",
	 "workloadCredentials": {
	  "PROJECT_ID.svc.id.goog": {
	   "metadata": {
	    "workload_creds_dir_path": "/var/run/secrets/workload-spiffe-credentials"
	   },
	   "certificatePem": "-----BEGIN CERTIFICATE-----datahere-----END CERTIFICATE-----",
	   "privateKeyPem": "-----BEGIN PRIVATE KEY-----datahere-----END PRIVATE KEY-----"
	  }
	 }
	}
*/

// WorkloadIdentities represents Workload Identities in metadata.
type WorkloadIdentities struct {
	Status              string
	WorkloadCredentials map[string]WorkloadCredential
}

// UnmarshalJSON is a custom JSON unmarshaller for WorkloadIdentities.
func (wi *WorkloadIdentities) UnmarshalJSON(b []byte) error {
	tmp := map[string]json.RawMessage{}
	err := json.Unmarshal(b, &tmp)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(tmp["status"], &wi.Status); err != nil {
		return err
	}

	wi.WorkloadCredentials = map[string]WorkloadCredential{}
	wcs := map[string]json.RawMessage{}
	if err := json.Unmarshal(tmp["workloadCredentials"], &wcs); err != nil {
		return err
	}

	for domain, value := range wcs {
		wc := WorkloadCredential{}
		err := json.Unmarshal(value, &wc)
		if err != nil {
			return err
		}
		wi.WorkloadCredentials[domain] = wc
	}

	return nil
}

// WorkloadCredential represents Workload Credentials in metadata.
type WorkloadCredential struct {
	Metadata       Metadata
	CertificatePem string
	PrivateKeyPem  string
}

/*
metadata key instance/workload-trusted-root-certs

	{
	 "status": "OK",
	 "rootCertificates": {
	  "PROJECT.svc.id.goog": {
	   "metadata": {
	    "workload_creds_dir_path": "/var/run/secrets/workload-spiffe-credentials"
	   },
	   "rootCertificatesPem": "-----BEGIN CERTIFICATE-----datahere-----END CERTIFICATE-----"
	  }
	 }
	}
*/

// WorkloadTrustedRootCerts represents Workload Trusted Root Certs in metadata.
type WorkloadTrustedRootCerts struct {
	Status           string
	RootCertificates map[string]RootCertificate
}

// UnmarshalJSON is a custom JSON unmarshaller for WorkloadTrustedRootCerts
func (wtrc *WorkloadTrustedRootCerts) UnmarshalJSON(b []byte) error {
	tmp := map[string]json.RawMessage{}
	err := json.Unmarshal(b, &tmp)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(tmp["status"], &wtrc.Status); err != nil {
		return err
	}

	wtrc.RootCertificates = map[string]RootCertificate{}
	rcs := map[string]json.RawMessage{}
	if err := json.Unmarshal(tmp["rootCertificates"], &rcs); err != nil {
		return err
	}

	for domain, value := range rcs {
		rc := RootCertificate{}
		err := json.Unmarshal(value, &rc)
		if err != nil {
			return err
		}
		wtrc.RootCertificates[domain] = rc
	}

	return nil
}

// RootCertificate represents a Root Certificate in metadata
type RootCertificate struct {
	Metadata            Metadata
	RootCertificatesPem string
}

// Metadata represents Metadata in metadata
type Metadata struct {
	WorkloadCredsDirPath string
}

func main() {
	ctx := context.Background()

	opts := logger.LogOpts{
		LoggerName:     programName,
		FormatFunction: logFormat,
		// No need for syslog.
		DisableLocalLogging: true,
	}

	opts.Writers = []io.Writer{os.Stderr}
	logger.Init(ctx, opts)
	defer logger.Infof("Done")

	// TODO: prune old dirs
	if err := refreshCreds(); err != nil {
		logger.Fatalf("Error refreshCreds: %v", err.Error())
	}

}

func refreshCreds() error {
	project, err := getMetadata("project/project-id")
	if err != nil {
		return fmt.Errorf("Error getting project ID: %v", err)
	}

	// Get status first so it can be written even when other endpoints are empty.
	certConfigStatus, err := getMetadata("instance/workload-certificates-config-status")
	if err != nil {
		// Return success when certs are not configured to avoid unnecessary systemd failed units.
		logger.Infof("Error getting config status, workload certificates may not be configured: %v", err)
		return nil
	}

	domain := fmt.Sprintf("%s.svc.id.goog", project)
	logger.Infof("Rotating workload credentials for trust domain %s", domain)

	now := time.Now().Format(time.RFC3339)
	contentDir := fmt.Sprintf("%s-%s", contentDirPrefix, now)
	tempSymlink := fmt.Sprintf("%s-%s", tempSymlinkPrefix, now)

	logger.Infof("Creating timestamp contents dir %s", contentDir)

	if err := os.MkdirAll(contentDir, 0755); err != nil {
		return fmt.Errorf("Error creating contents dir: %v", err)
	}

	// Write config_status first even if remaining endpoints are empty.
	if err := os.WriteFile(fmt.Sprintf("%s/config_status", contentDir), certConfigStatus, 0644); err != nil {
		return fmt.Errorf("Error writing config_status: %v", err)
	}

	// Handles the edge case where the config values provided for the first time may be invalid. This ensures
	// that the symlink directory alwasys exists and contains the config_status to surface config errors to the VM.
	if _, err := os.Stat(symlink); os.IsNotExist(err) {
		logger.Infof("Creating new symlink %s", symlink)

		if err := os.Symlink(contentDir, symlink); err != nil {
			return fmt.Errorf("Error creating symlink: %v", err)
		}
	}

	// Now get the rest of the content.
	wisMd, err := getMetadata("instance/workload-identities")
	if err != nil {
		return fmt.Errorf("Error getting workload-identities: %v", err)
	}

	wtrcsMd, err := getMetadata("instance/workload-trusted-root-certs")
	if err != nil {
		return fmt.Errorf("Error getting workload-trusted-root-certs: %v", err)
	}

	wis := WorkloadIdentities{}
	if err := json.Unmarshal(wisMd, &wis); err != nil {
		return fmt.Errorf("Error unmarshaling workload identities response: %v", err)
	}

	wtrcs := WorkloadTrustedRootCerts{}
	if err := json.Unmarshal(wtrcsMd, &wtrcs); err != nil {
		return fmt.Errorf("Error unmarshaling workload trusted root certs: %v", err)
	}

	if err := os.WriteFile(fmt.Sprintf("%s/certificates.pem", contentDir), []byte(wis.WorkloadCredentials[domain].CertificatePem), 0644); err != nil {
		return fmt.Errorf("Error writing certificates.pem: %v", err)
	}

	if err := os.WriteFile(fmt.Sprintf("%s/private_key.pem", contentDir), []byte(wis.WorkloadCredentials[domain].PrivateKeyPem), 0644); err != nil {
		return fmt.Errorf("Error writing private_key.pem: %v", err)
	}

	if err := os.WriteFile(fmt.Sprintf("%s/ca_certificates.pem", contentDir), []byte(wtrcs.RootCertificates[domain].RootCertificatesPem), 0644); err != nil {
		return fmt.Errorf("Error writing ca_certificates.pem: %v", err)
	}

	if err := os.Symlink(contentDir, tempSymlink); err != nil {
		return fmt.Errorf("Error creating temporary link: %v", err)
	}

	oldTarget, err := os.Readlink(symlink)
	if err != nil {
		logger.Infof("Error reading existing symlink: %v\n", err)
		oldTarget = ""
	}

	// Only rotate on success of all steps above.
	logger.Infof("Rotating symlink %s", symlink)

	if err := os.Rename(tempSymlink, symlink); err != nil {
		return fmt.Errorf("Error rotating target link: %v", err)
	}

	// Clean up previous contents dir.
	newTarget, err := os.Readlink(symlink)
	if err != nil {
		return fmt.Errorf("Error reading new symlink: %v, unable to remove old symlink target", err)
	}
	if oldTarget != newTarget {
		logger.Infof("Removing old content dir %s", oldTarget)
		if err := os.RemoveAll(oldTarget); err != nil {
			return fmt.Errorf("Failed to remove old symlink target: %v", err)
		}
	}

	return nil
}
