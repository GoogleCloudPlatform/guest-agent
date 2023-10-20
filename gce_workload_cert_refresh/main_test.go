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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/google/go-cmp/cmp"
)

const (
	workloadRespTpl = `
	{
		"status": "OK",
		"workloadCredentials": {
			"%s": {
				"certificatePem": "%s",
				"privateKeyPem": "%s"
			}
		}
	}
	`
	trustAnchorRespTpl = `
	{
		"status": "Ok",
		"trustAnchors": {
			"%s": {
				"trustAnchorsPem": "%s"
			},
			"%s": {
				"trustAnchorsPem": "%s"
			}
		}
	}
	`
	testConfigStatusResp = `
	{
		"status": "Ok",
	}
	`
)

func TestWorkloadIdentitiesUnmarshal(t *testing.T) {
	certPem := "-----BEGIN CERTIFICATE-----datahere-----END CERTIFICATE-----"
	pvtPem := "-----BEGIN PRIVATE KEY-----datahere-----END PRIVATE KEY-----"
	spiffe := "spiffe://12345.global.67890.workload.id.goog/ns/NAMESPACE_ID/sa/MANAGED_IDENTITY_ID"

	resp := fmt.Sprintf(workloadRespTpl, spiffe, certPem, pvtPem)
	want := WorkloadIdentities{
		Status: "OK",
		WorkloadCredentials: map[string]WorkloadCredential{
			spiffe: {
				CertificatePem: certPem,
				PrivateKeyPem:  pvtPem,
			},
		},
	}

	got := WorkloadIdentities{}
	if err := json.Unmarshal([]byte(resp), &got); err != nil {
		t.Errorf("WorkloadIdentities.UnmarshalJSON(%s) failed unexpectedly with error: %v", resp, err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Workload identities diff (-want +got):\n%s", diff)
	}
}

func TestTrustAnchorsUnmarshal(t *testing.T) {
	domain1 := "12345.global.67890.workload.id.goog"
	pem1 := "-----BEGIN CERTIFICATE-----datahere1-----END CERTIFICATE-----"
	domain2 := "PEER_SPIFFE_TRUST_DOMAIN_2"
	pem2 := "-----BEGIN CERTIFICATE-----datahere2-----END CERTIFICATE-----"

	resp := fmt.Sprintf(trustAnchorRespTpl, domain1, pem1, domain2, pem2)
	want := WorkloadTrustedAnchors{
		Status: "Ok",
		TrustAnchors: map[string]TrustAnchor{
			domain1: {
				TrustAnchorsPem: pem1,
			},
			domain2: {
				TrustAnchorsPem: pem2,
			},
		},
	}

	got := WorkloadTrustedAnchors{}
	if err := json.Unmarshal([]byte(resp), &got); err != nil {
		t.Errorf("WorkloadTrustedRootCerts.UnmarshalJSON(%s) failed unexpectedly with error: %v", resp, err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Workload trusted anchors diff (-want +got):\n%s", diff)
	}
}

func TestWriteTrustAnchors(t *testing.T) {
	spiffe := "spiffe://12345.global.67890.workload.id.goog/ns/NAMESPACE_ID/sa/MANAGED_IDENTITY_ID"
	domain1 := "12345.global.67890.workload.id.goog"
	pem1 := "-----BEGIN CERTIFICATE-----datahere1-----END CERTIFICATE-----"
	domain2 := "PEER_SPIFFE_TRUST_DOMAIN_2"
	pem2 := "-----BEGIN CERTIFICATE-----datahere2-----END CERTIFICATE-----"

	resp := fmt.Sprintf(trustAnchorRespTpl, domain1, pem1, domain2, pem2)
	dir := t.TempDir()
	if err := writeTrustAnchors([]byte(resp), dir, spiffe); err != nil {
		t.Errorf("writeTrustAnchors(%s,%s,%s) failed unexpectedly with error %v", resp, dir, spiffe, err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "ca_certificates.pem"))
	if err != nil {
		t.Errorf("failed to read file at %s with error: %v", filepath.Join(dir, "ca_certificates.pem"), err)
	}
	if string(got) != pem1 {
		t.Errorf("writeTrustAnchors(%s,%s,%s) wrote %q, expected to write %q", resp, dir, spiffe, string(got), pem1)
	}
}

func TestWriteWorkloadIdentities(t *testing.T) {
	certPem := "-----BEGIN CERTIFICATE-----datahere-----END CERTIFICATE-----"
	pvtPem := "-----BEGIN PRIVATE KEY-----datahere-----END PRIVATE KEY-----"
	spiffe := "spiffe://12345.global.67890.workload.id.goog/ns/NAMESPACE_ID/sa/MANAGED_IDENTITY_ID"

	resp := fmt.Sprintf(workloadRespTpl, spiffe, certPem, pvtPem)
	dir := t.TempDir()

	gotID, err := writeWorkloadIdentities(dir, []byte(resp))
	if err != nil {
		t.Errorf("writeWorkloadIdentities(%s,%s) failed unexpectedly with error %v", dir, resp, err)
	}
	if gotID != spiffe {
		t.Errorf("writeWorkloadIdentities(%s,%s) = %s, want %s", dir, resp, gotID, spiffe)
	}

	gotCertPem, err := os.ReadFile(filepath.Join(dir, "certificates.pem"))
	if err != nil {
		t.Errorf("failed to read file at %s with error: %v", filepath.Join(dir, "certificates.pem"), err)
	}
	if string(gotCertPem) != certPem {
		t.Errorf("writeWorkloadIdentities(%s,%s) wrote %q, expected to write %q", dir, resp, string(gotCertPem), certPem)
	}

	gotPvtPem, err := os.ReadFile(filepath.Join(dir, "private_key.pem"))
	if err != nil {
		t.Errorf("failed to read file at %s with error: %v", filepath.Join(dir, "private_key.pem"), err)
	}
	if string(gotPvtPem) != pvtPem {
		t.Errorf("writeWorkloadIdentities(%s,%s) wrote %q, expected to write %q", dir, resp, string(gotPvtPem), pvtPem)
	}
}

func TestFindDomainError(t *testing.T) {
	anchors := map[string]TrustAnchor{
		"67890.global.12345.workload.id.goog": {},
		"55555.global.67890.workload.id.goog": {},
	}
	spiffeID := "spiffe://12345.global.67890.workload.id.goog/ns/NAMESPACE_ID/sa/MANAGED_IDENTITY_ID"

	if _, err := findDomain(anchors, spiffeID); err == nil {
		t.Errorf("findDomain(%+v, %s) succeded for unknown anchors, want error", anchors, spiffeID)
	}
}

func TestFindDomain(t *testing.T) {
	tests := []struct {
		desc     string
		anchors  map[string]TrustAnchor
		spiffeID string
		want     string
	}{
		{
			desc:     "single_trust_anchor",
			anchors:  map[string]TrustAnchor{"12345.global.67890.workload.id.goog": {}},
			spiffeID: "spiffe://12345.global.67890.workload.id.goog/ns/NAMESPACE_ID/sa/MANAGED_IDENTITY_ID",
			want:     "12345.global.67890.workload.id.goog",
		},
		{
			desc: "multiple_trust_anchor",
			anchors: map[string]TrustAnchor{
				"67890.global.12345.workload.id.goog": {},
				"12345.global.67890.workload.id.goog": {},
			},
			spiffeID: "spiffe://12345.global.67890.workload.id.goog/ns/NAMESPACE_ID/sa/MANAGED_IDENTITY_ID",
			want:     "12345.global.67890.workload.id.goog",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			got, err := findDomain(test.anchors, test.spiffeID)
			if err != nil {
				t.Errorf("findDomain(%+v, %s) failed unexpectedly with error: %v", test.anchors, test.spiffeID, err)
			}
			if got != test.want {
				t.Errorf("findDomain(%+v, %s) = %s, want %s", test.anchors, test.spiffeID, got, test.want)
			}
		})
	}
}

func TestIsEnabled(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		desc    string
		enabled string
		want    bool
		err     string
	}{
		{
			desc:    "attr_correctly_added",
			enabled: "true",
			want:    true,
		},
		{
			desc:    "attr_incorrectly_added",
			enabled: "blaah",
			want:    false,
		},
		{
			desc: "attr_not_added",
			want: false,
			err:  enableWorkloadCertsKey,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			mdsClient = &mdsTestClient{enabled: test.enabled, throwErrOn: test.err}
			if got := isEnabled(ctx); got != test.want {
				t.Errorf("isEnabled(ctx) = %t, want %t", got, test.want)
			}
		})
	}
}

// mdsTestClient is fake client to stub MDS response in unit tests.
type mdsTestClient struct {
	// Is credential generation enabled.
	enabled string
	// Workload template.
	spiffe, certPem, pvtPem string
	// Trust Anchor template.
	domain1, pem1, domain2, pem2 string
	// Throw error on MDS request for "key".
	throwErrOn string
}

func (mds *mdsTestClient) Get(ctx context.Context) (*metadata.Descriptor, error) {
	return nil, fmt.Errorf("Get() not yet implemented")
}

func (mds *mdsTestClient) GetKey(ctx context.Context, key string, headers map[string]string) (string, error) {
	if mds.throwErrOn == key {
		return "", fmt.Errorf("this is fake error for testing")
	}

	switch key {
	case enableWorkloadCertsKey:
		return mds.enabled, nil
	case configStatusKey:
		return testConfigStatusResp, nil
	case workloadIdentitiesKey:
		return fmt.Sprintf(workloadRespTpl, mds.spiffe, mds.certPem, mds.pvtPem), nil
	case trustAnchorsKey:
		return fmt.Sprintf(trustAnchorRespTpl, mds.domain1, mds.pem1, mds.domain2, mds.pem2), nil
	default:
		return "", fmt.Errorf("unknown key %q", key)
	}
}

func (mds *mdsTestClient) GetKeyRecursive(ctx context.Context, key string) (string, error) {
	return "", fmt.Errorf("GetKeyRecursive() not yet implemented")
}

func (mds *mdsTestClient) Watch(ctx context.Context) (*metadata.Descriptor, error) {
	return nil, fmt.Errorf("Watch() not yet implemented")
}

func (mds *mdsTestClient) WriteGuestAttributes(ctx context.Context, key string, value string) error {
	return fmt.Errorf("WriteGuestattributes() not yet implemented")
}

func TestRefreshCreds(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	// Templates to use in iterations.
	spiffeTpl := "spiffe://12345.global.67890.workload.id.goog.%d/ns/NAMESPACE_ID/sa/MANAGED_IDENTITY_ID"
	domain1Tpl := "12345.global.67890.workload.id.goog.%d"
	pem1Tpl := "-----BEGIN CERTIFICATE-----datahere1.%d-----END CERTIFICATE-----"
	domain2 := "PEER_SPIFFE_TRUST_DOMAIN_2_IGNORE"
	pem2Tpl := "-----BEGIN CERTIFICATE-----datahere2.%d-----END CERTIFICATE-----"
	certPemTpl := "-----BEGIN CERTIFICATE-----datahere.%d-----END CERTIFICATE-----"
	pvtPemTpl := "-----BEGIN PRIVATE KEY-----datahere.%d-----END PRIVATE KEY-----"

	contentPrefix := filepath.Join(tmp, "workload-spiffe-contents")
	tmpSymlinkPrefix := filepath.Join(tmp, "workload-spiffe-symlink")
	link := filepath.Join(tmp, "workload-spiffe-credentials")
	out := outputOpts{contentPrefix, tmpSymlinkPrefix, link}

	// Run refresh creds thrice to test updates.
	// Link (workload-spiffe-credentials) should always refer to the updated content
	// and previous directories should be removed.
	for i := 1; i <= 3; i++ {
		timeNow = func() string { return fmt.Sprintf("%d", i) }
		spiffe := fmt.Sprintf(spiffeTpl, i)
		domain1 := fmt.Sprintf(domain1Tpl, i)
		pem1 := fmt.Sprintf(pem1Tpl, i)
		pem2 := fmt.Sprintf(pem2Tpl, i)
		certPem := fmt.Sprintf(certPemTpl, i)
		pvtPem := fmt.Sprintf(pvtPemTpl, i)

		mdsClient = &mdsTestClient{
			spiffe:  spiffe,
			certPem: certPem,
			pvtPem:  pvtPem,
			domain1: domain1,
			pem1:    pem1,
			domain2: domain2,
			pem2:    pem2,
		}

		if err := refreshCreds(ctx, out); err != nil {
			t.Errorf("refreshCreds(ctx, %+v) failed unexpectedly with error: %v", out, err)
		}

		// Verify all files are created with the content as expected.
		tests := []struct {
			path    string
			content string
		}{
			{
				path:    filepath.Join(link, "ca_certificates.pem"),
				content: pem1,
			},
			{
				path:    filepath.Join(link, "certificates.pem"),
				content: certPem,
			},
			{
				path:    filepath.Join(link, "private_key.pem"),
				content: pvtPem,
			},
			{
				path:    filepath.Join(link, "config_status"),
				content: testConfigStatusResp,
			},
		}

		for _, test := range tests {
			t.Run(test.path, func(t *testing.T) {
				got, err := os.ReadFile(test.path)
				if err != nil {
					t.Errorf("failed to read expected file %q and content %q with error: %v", test.path, test.content, err)
				}
				if string(got) != test.content {
					t.Errorf("refreshCreds(ctx, %+v) wrote %q, want content %q", out, string(got), test.content)
				}
			})
		}

		// Verify the symlink was created and references the right destination directory.
		want := fmt.Sprintf("%s-%d", contentPrefix, i)
		got, err := os.Readlink(link)
		if err != nil {
			t.Errorf("os.Readlink(%s) failed unexpectedly with error %v", link, err)
		}
		if got != want {
			t.Errorf("os.Readlink(%s) = %s, want %s", link, got, want)
		}

		// If its not first run make sure prev creds are deleted.
		if i > 1 {
			prevDir := fmt.Sprintf("%s-%d", contentPrefix, i-1)
			if _, err := os.Stat(prevDir); err == nil {
				t.Errorf("os.Stat(%s) succeeded on prev content directory, want error", prevDir)
			}
		}
	}
}

func TestRefreshCredsError(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	// Templates to use in iterations.
	spiffe := "spiffe://12345.global.67890.workload.id.goog/ns/NAMESPACE_ID/sa/MANAGED_IDENTITY_ID"
	domain1 := "12345.global.67890.workload.id.goog"
	pem1 := "-----BEGIN CERTIFICATE-----datahere1-----END CERTIFICATE-----"
	domain2 := "PEER_SPIFFE_TRUST_DOMAIN_2_IGNORE"
	pem2 := "-----BEGIN CERTIFICATE-----datahere2-----END CERTIFICATE-----"
	certPem := "-----BEGIN CERTIFICATE-----datahere-----END CERTIFICATE-----"
	pvtPem := "-----BEGIN PRIVATE KEY-----datahere-----END PRIVATE KEY-----"

	contentPrefix := filepath.Join(tmp, "workload-spiffe-contents")
	tmpSymlinkPrefix := filepath.Join(tmp, "workload-spiffe-symlink")
	link := filepath.Join(tmp, "workload-spiffe-credentials")
	out := outputOpts{contentPrefix, tmpSymlinkPrefix, link}

	client := &mdsTestClient{
		spiffe:  spiffe,
		certPem: certPem,
		pvtPem:  pvtPem,
		domain1: domain1,
		pem1:    pem1,
		domain2: domain2,
		pem2:    pem2,
	}

	mdsClient = client

	// Run refresh creds twice. First run would succeed and second would fail. Verify all
	// creds generated on the first run are present as is after failed second run.
	for i := 1; i <= 2; i++ {
		timeNow = func() string { return fmt.Sprintf("%d", i) }

		if i == 1 {
			// First run should succeed.
			if err := refreshCreds(ctx, out); err != nil {
				t.Errorf("refreshCreds(ctx, %+v) failed unexpectedly with error: %v", out, err)
			}
		} else if i == 2 {
			// Second run should fail. Fail in getting last metadata entry.
			client.throwErrOn = trustAnchorsKey
			if err := refreshCreds(ctx, out); err == nil {
				t.Errorf("refreshCreds(ctx, %+v) succeeded for fake metadata error, should've failed", out)
			}
		}

		// Verify all files are created and are still present with the content as expected.
		tests := []struct {
			path    string
			content string
		}{
			{
				path:    filepath.Join(link, "ca_certificates.pem"),
				content: pem1,
			},
			{
				path:    filepath.Join(link, "certificates.pem"),
				content: certPem,
			},
			{
				path:    filepath.Join(link, "private_key.pem"),
				content: pvtPem,
			},
			{
				path:    filepath.Join(link, "config_status"),
				content: testConfigStatusResp,
			},
		}

		for _, test := range tests {
			t.Run(test.path, func(t *testing.T) {
				got, err := os.ReadFile(test.path)
				if err != nil {
					t.Errorf("failed to read expected file %q and content %q with error: %v", test.path, test.content, err)
				}
				if string(got) != test.content {
					t.Errorf("refreshCreds(ctx, %+v) wrote %q, want content %q", out, string(got), test.content)
				}
			})
		}

		// Verify the symlink was created and references the same destination directory.
		want := fmt.Sprintf("%s-%d", contentPrefix, 1)
		got, err := os.Readlink(link)
		if err != nil {
			t.Errorf("os.Readlink(%s) failed unexpectedly with error %v", link, err)
		}
		if got != want {
			t.Errorf("os.Readlink(%s) = %s, want %s", link, got, want)
		}
	}
}
