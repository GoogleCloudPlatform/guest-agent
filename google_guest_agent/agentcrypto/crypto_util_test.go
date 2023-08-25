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

package agentcrypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

const validCertPEM = `
-----BEGIN CERTIFICATE-----
MIIDujCCAqKgAwIBAgIIE31FZVaPXTUwDQYJKoZIhvcNAQEFBQAwSTELMAkGA1UE
BhMCVVMxEzARBgNVBAoTCkdvb2dsZSBJbmMxJTAjBgNVBAMTHEdvb2dsZSBJbnRl
cm5ldCBBdXRob3JpdHkgRzIwHhcNMTQwMTI5MTMyNzQzWhcNMTQwNTI5MDAwMDAw
WjBpMQswCQYDVQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTEWMBQGA1UEBwwN
TW91bnRhaW4gVmlldzETMBEGA1UECgwKR29vZ2xlIEluYzEYMBYGA1UEAwwPbWFp
bC5nb29nbGUuY29tMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEfRrObuSW5T7q
5CnSEqefEmtH4CCv6+5EckuriNr1CjfVvqzwfAhopXkLrq45EQm8vkmf7W96XJhC
7ZM0dYi1/qOCAU8wggFLMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjAa
BgNVHREEEzARgg9tYWlsLmdvb2dsZS5jb20wCwYDVR0PBAQDAgeAMGgGCCsGAQUF
BwEBBFwwWjArBggrBgEFBQcwAoYfaHR0cDovL3BraS5nb29nbGUuY29tL0dJQUcy
LmNydDArBggrBgEFBQcwAYYfaHR0cDovL2NsaWVudHMxLmdvb2dsZS5jb20vb2Nz
cDAdBgNVHQ4EFgQUiJxtimAuTfwb+aUtBn5UYKreKvMwDAYDVR0TAQH/BAIwADAf
BgNVHSMEGDAWgBRK3QYWG7z2aLV29YG2u2IaulqBLzAXBgNVHSAEEDAOMAwGCisG
AQQB1nkCBQEwMAYDVR0fBCkwJzAloCOgIYYfaHR0cDovL3BraS5nb29nbGUuY29t
L0dJQUcyLmNybDANBgkqhkiG9w0BAQUFAAOCAQEAH6RYHxHdcGpMpFE3oxDoFnP+
gtuBCHan2yE2GRbJ2Cw8Lw0MmuKqHlf9RSeYfd3BXeKkj1qO6TVKwCh+0HdZk283
TZZyzmEOyclm3UGFYe82P/iDFt+CeQ3NpmBg+GoaVCuWAARJN/KfglbLyyYygcQq
0SgeDh8dRKUiaW3HQSoYvTvdTuqzwK4CXsr3b5/dAOY8uMuG/IAR3FgwTbZ1dtoW
RvOTa8hYiU6A475WuZKyEHcwnGYe57u2I2KbMgcKjPniocj4QzgYsVAVKW3IwaOh
yE+vPxsiUkvQHdO2fojCkY8jg70jxM+gu59tPDNbw3Uh/2Ij310FgTHsnGQMyA==
-----END CERTIFICATE-----`

const invalidCertPEM = `
-----BEGIN CERTIFICATE-----
MIIDujCCAqKgAwIBAgIIE31FZVaPXTUwDQYJKoZIhvcNAQEFBQAwSTELMAkGA1UE
BhMCVVMxEzARBgNVBAoTCkdvb2dsZSBJbmMxJTAjBgNVBAMTHEdvb2dsZSBJbnRl
cm5ldCBBdXRob3JpdHkgRzIwHhcNMTQwMTI5MTMyNzQzWhcNMTQwNTI5MDAwMDAw
gtuBCHan2yE2GRbJ2Cw8Lw0MmuKqHlf9RSeYfd3BXeKkj1qO6TVKwCh+0HdZk283
TZZyzmEOyclm3UGFYe82P/iDFt+CeQ3NpmBg+GoaVCuWAARJN/KfglbLyyYygcQq
yE+vPxsiUkvQHdO2fojCkY8jg70jxM+gu59tPDNbw3Uh/2Ij310FgTHsnGQMyA==
-----END CERTIFICATE-----`

func TestParseCertificate(t *testing.T) {
	if _, err := parseCertificate([]byte(validCertPEM)); err != nil {
		t.Errorf("parseCertificate(%s) failed unexpectedly with error: %v", validCertPEM, err)
	}
}

func TestParseCertificateError(t *testing.T) {
	if _, err := parseCertificate([]byte(invalidCertPEM)); err == nil {
		t.Errorf("parseCertificate(%s) succeeded unexpectedly for invalid certificate, want error", invalidCertPEM)
	}
}

func TestEncryptDecrypt(t *testing.T) {
	// 32 byte key.
	key := []byte("AES256Key-32Characters1234567890")
	plaintext := []byte("testplaintext")

	ciphertext, err := encrypt(key, plaintext, nil)
	if err != nil {
		t.Errorf("encrypt(%s,%s) failed unexpectedly with error: %v", key, plaintext, err)
	}

	got, err := decrypt(key, ciphertext, nil)
	if err != nil {
		t.Errorf("decrypt(%s,%s) failed unexpectedly with error: %v", string(key), string(ciphertext), err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Errorf("decrypt(%s,%s) = %s want %s", string(key), string(ciphertext), string(got), string(plaintext))
	}
}

const cacert = `
-----BEGIN CERTIFICATE-----
MIIDbTCCAlWgAwIBAgIUFTF0rnA2LoffIJEKSh+rQcmehSIwDQYJKoZIhvcNAQEL
BQAwRTELMAkGA1UEBhMCQVUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM
GEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDAgFw0yMzA3MjgyMjI3MTdaGA8zMDIy
MTEyODIyMjcxN1owRTELMAkGA1UEBhMCQVUxEzARBgNVBAgMClNvbWUtU3RhdGUx
ITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDCCASIwDQYJKoZIhvcN
AQEBBQADggEPADCCAQoCggEBAKiWs/hXZgTtFkpFvdXO/nLpLJSCq5rwqAJauTmj
Y78Za1QmgaqCcguakKf/hb+MxRL9h9qJVBAQkNZv0nChoTJyD6YF5hh4DDrQCPuh
1wvVsUhUllIbKsJbjQmdkOb3A5fMoe1ki4BLsr1CtJfJVj1+ifR+7hNkD3fW2sls
XZlrNZRmbMKq84KRBWTSSxhjYZGd2cCGpecJ2fWuva9QhairdnB4TORAfjiyH+5v
GEwXWC9gyDIIXWDG/kxwDDnh7kub0UsMf/neLv0hejpW/pfmvt32IoMaTEGFDaj7
lhTo7UVQw/XCFWqElsi8gHXR+/UdzbON5a8GiyjWJq5SThsCAwEAAaNTMFEwHQYD
VR0OBBYEFPvD/mUJgRgzLmWCD5zFNglzMb55MB8GA1UdIwQYMBaAFPvD/mUJgRgz
LmWCD5zFNglzMb55MA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEB
ABUr0RNasEZ39wM1CDE/qZDo+gBMGWH8gE/x152KPvzvJZmI96LkYuKzmbIrvogJ
rfGYkAP2LYc8bX6zs4e2VycF0pml7ARKHyinzDdcwXOKzg9gGanoZw4wXEtxfWSl
GbmNplmhmMpEnrtTNeDbqGWvmO/1fziNduimNVVu1iltNYEszE/ch8AlMT7flfNm
JnhzvUUnGeXDiWUIJdneDfXopatOboL/0HimnfNK6//NKUlMCQOfNbNND+372jhK
B3V0o4sGyoh8/Jlas+SqEtVKv+jfNfAG0urLzJc4Zn2uc2chpZnD8DxkmzA5nJCf
+5xLOukYO2I5KMgyYkYNUXs=
-----END CERTIFICATE-----
`

func TestVerifySign(t *testing.T) {
	// Fake self signed ceritificates for testing.
	client := `
-----BEGIN CERTIFICATE-----
MIIDADCCAegCAQEwDQYJKoZIhvcNAQELBQAwRTELMAkGA1UEBhMCQVUxEzARBgNV
BAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5IEx0
ZDAgFw0yMzA3MjgyMjI3NDZaGA8zMDIyMTEyODIyMjc0NlowRTELMAkGA1UEBhMC
QVUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdp
dHMgUHR5IEx0ZDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBANxAeYva
w0J52P9e2IXPEyyJncOKOSGxCWqf0yHpuQ95STMMrgVSodN1Jdrpd2DPOqOYIriK
uHw1L4DCm5/yGP7WznN/JOORoJTZ5qJXBXNNQZxf1d5qJeBWtFnVv2pAPwFM/c8j
YNFCjTxAHjEMfZN0uXt1ELa6OkYCwxxiVq+Z6QT47xhvQHBzCFhPCaXy8ezvBanU
m2AJ2O3HYu9JCy37baDsyVlhrt1qRTKG3JFCgqGEs2vkFo25ebv0Nq8crtT8J6wz
YWbIpB56v+299f7jqStjljapG+nMrSbk8BRvMPAlg8Hg6mJ3RQgW5DgKE/7BSgue
U2oF7ODsKjF5xesCAwEAATANBgkqhkiG9w0BAQsFAAOCAQEAnbriOFcw2b/1zqfr
M3FK3TAjcD+InKpY/bNjhdbfCRgO3WYdnWsVU437vKkiJH0tAOAzdR3Yd5xpLkuS
uUjIiY4VTR10tZxmFuyAq87NZx4zMCJ7XNRQtDU5o+EUyZVV7l2fwbS31unCZQn5
10nSg5TQE3kZ4u+3x4PZgSbMhIY8P5Q/ZjAKVKk0hnT1ClQ5LQwetcDR7KMq9DpE
sC6BD/ElCi0RJrZqVVccAhumf9NBk/qWf1E4njlmYLmqNrfZGEfbxiKAOsZFYaCV
p/45VCE10OS3mYEFwJmQjH5NoqaSGxWU28reovEEmrDFoGYfkMQbxZzay0LURXt7
aSrX4A==
-----END CERTIFICATE-----
	`

	root := filepath.Join(t.TempDir(), "root.ca")
	if err := os.WriteFile(root, []byte(cacert), 0644); err != nil {
		t.Fatalf("Failed to setup test CA cert file: %v", err)
	}

	if err := verifySign([]byte(client), root); err != nil {
		t.Errorf("verifySign failed unexpectedly with error: %v", err)
	}
}

func TestVerifySignError(t *testing.T) {
	// Fake invalid self signed ceritificates for testing.
	client := `
-----BEGIN CERTIFICATE-----
MIIDADCCAegCAQEwDQYJKoZIhvcNAQELBQAwRTELMAkGA1UEBhMCQVUxEzARBgNV
BAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5IEx0
ZDAgFw0yMzA3MjgyMjM4NTBaGA8zMDIyMTEyODIyMzg1MFowRTELMAkGA1UEBhMC
QVUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdp
dHMgUHR5IEx0ZDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAKW78MTO
TO6F5/68B/e3qQYHRJ1OYv43+1U503fTnkQIyf1KtZvABmPIXmckDJAlTmtD8WQp
lKVxCtSJ0aNNwj2epFBo/CoO5gIuFWjjxkiTfneCDxTF4SxqzVzvNuT0JtsG/Ysd
2b2GCIhHbqM7YLCol6V++SSO+NTR2kUx6RQ+f4vvnKWfv2pRgl8jHhq29U71BKtY
k1rH6kd13QOl71IMY3E2SRB9rONe0/lgrVyaKKJto5a0WVDgrjZP4e+0lpvtD3jN
JOFcJYrrDHAdxjQMEqbT4b1+M/HEOwJMDI2nZAI2exDmN8R2Wburp7hKNeygA4AM
7x91qP9jNfmS/wUCAwEAATANBgkqhkiG9w0BAQsFAAOCAQEAfv4sxcTTu66KU1h2
ol2DY2JQSywsWY37cfrdL9D1u2sf/MSyAN+i6XcwG/WReoPS8jLFPWJBVHYFQOWt
OVw93lVfFlFfz1GojCiddGZxZTWLhKSVvnkRVuRlOD7ph6UjowTUe+JrK5bh/pT8
m+g/HmvC/0V5fgQFvtujjc3DkHzKk7HXj39OFsLVGvNDdI6f7+mdc7ib2qs5/uQt
T+CR3W1LK08doMc8/SG74Q1i8eU1/AcX1QK1SQqX/TBF8EpCDII8BMTBp/KPp6JV
GPQpdL4CXXRtVxz5wf/GuMKbgBe9nPh9bFoRrmH6B/LK9dckvZJG9wT7lzuCXZ3d
zBbQ2g==
-----END CERTIFICATE-----
	`

	root := filepath.Join(t.TempDir(), "root.ca")
	if err := os.WriteFile(root, []byte(cacert), 0644); err != nil {
		t.Fatalf("Failed to setup test CA cert file: %v", err)
	}

	tests := []struct {
		name   string
		client string
	}{
		{
			name:   "invalid_signed_client",
			client: client,
		},
		{
			name:   "incorrectly_formatted_client",
			client: invalidCertPEM,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := verifySign([]byte(test.client), root); err == nil {
				t.Errorf("verifySign succeeded unexpectedly for %s, want error", test.name)
			}
		})
	}
}

func TestSerialNumber(t *testing.T) {
	f := filepath.Join(t.TempDir(), "cert")
	if err := os.WriteFile(f, []byte(validCertPEM), 0777); err != nil {
		t.Errorf("Failed to create test cert file: %v", err)
	}

	want := "137d4565568f5d35"
	got, err := serialNumber(f)

	if err != nil {
		t.Errorf("serialNumber(%s) failed unexpectedly with error: %v", f, err)
	}
	if got != want {
		t.Errorf("serialNumber(%s) = %s, want %s", f, got, want)
	}
}

func generatePrivateKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	x509Encoded, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("Failed to Marshal EC PrivateKey: %v", err)
	}

	return key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x509Encoded})
}

func TestParseECPrivateKey(t *testing.T) {
	key, pem := generatePrivateKey(t)
	got, err := parsePvtKey(pem)
	if err != nil {
		t.Errorf("parsePvtKey(%s) failed unexpectedly with error: %v", string(pem), err)
	}

	if !key.Equal(got) {
		t.Errorf("parsePvtKey(%s) parsed private key incorrectly", string(pem))
	}
}
