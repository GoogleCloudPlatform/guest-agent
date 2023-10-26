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
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"golang.org/x/sys/windows"
	"software.sslmate.com/src/go-pkcs12"
)

const (
	// rootCACertFileName is the root CA cert.
	rootCACertFileName = "mds-mtls-root.crt"
	// clientCredsFileName are client credentials, its basically the file
	// that has the EC private key and the client certificate concatenated.
	clientCredsFileName = "mds-mtls-client.key"
	// pfxFile stores client credentials in PFX format.
	pfxFile = "mds-mtls-client.key.pfx"
	// https://learn.microsoft.com/en-us/windows/win32/seccrypto/system-store-locations
	// my is predefined personal cert store.
	my = "MY"
	// root is predefined cert store for root trusted CA certs.
	root = "ROOT"
	// certificateIssuer is the issuer of client/root certificates for MDS mTLS.
	certificateIssuer = "google.internal"
	// maxCertEnumeration specifies the maximum number of times to search for a certificate
	// with a serial number from a given issuer before giving up.
	maxCertEnumeration = 5
)

var (
	// defaultCredsDir is the directory location for MTLS MDS credentials.
	defaultCredsDir = filepath.Join(os.Getenv("ProgramData"), "Google", "Compute Engine")
	prevCtx         *windows.CertContext
)

// writeRootCACert writes Root CA cert from UEFI variable to output file.
func (j *CredsJob) writeRootCACert(_ context.Context, cacert []byte, outputFile string) error {
	// Try to fetch previous certificate's serial number before it gets overwritten.
	num, err := serialNumber(outputFile)
	if err != nil {
		logger.Debugf("No previous MDS root certificate was found, will skip cleanup: %v", err)
	}

	if err := utils.SaferWriteFile(cacert, outputFile, 0644); err != nil {
		return err
	}

	x509Cert, err := parseCertificate(cacert)
	if err != nil {
		return fmt.Errorf("failed to parse root CA cert: %w", err)
	}

	// https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certcreatecertificatecontext
	certContext, err := windows.CertCreateCertificateContext(
		windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING,
		&x509Cert.Raw[0],
		uint32(len(x509Cert.Raw)))
	if err != nil {
		return fmt.Errorf("CertCreateCertificateContext returned: %v", err)
	}
	// https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certfreecertificatecontext
	defer windows.CertFreeCertificateContext(certContext)

	// Adds certificate to Root Trusted certificates.
	if err := addCtxToLocalSystemStore(root, certContext, uint32(windows.CERT_STORE_ADD_REPLACE_EXISTING)); err != nil {
		return fmt.Errorf("failed to store root cert ctx in store: %w", err)
	}

	// MDS root cert was not refreshed or there's no previous cert, nothing to do, return.
	if num == "" || fmt.Sprintf("%x", x509Cert.SerialNumber) == num {
		return nil
	}

	// Certificate is refreshed. Best effort to find the certcontext and delete it.
	// Don't throw error here, it would skip client credential generation which
	// may be about to expire.
	oldCtx, err := findCert(root, certificateIssuer, num)
	if err != nil {
		logger.Warningf("Failed to find previous MDS root certificate with error: %v", err)
		return nil
	}

	if err := deleteCert(oldCtx, root); err != nil {
		logger.Warningf("Failed to delete previous MDS root certificate(%s) with error: %v", num, err)
		return nil
	}

	return nil
}

// findCert finds and returns certificate issued by issuer with the serial number in the given the store.
func findCert(storeName, issuer, certID string) (*windows.CertContext, error) {
	logger.Infof("Searching for certificate with serial number %s in store %s by issuer %s", certID, storeName, issuer)

	st, err := windows.CertOpenStore(
		windows.CERT_STORE_PROV_SYSTEM,
		0,
		0,
		windows.CERT_SYSTEM_STORE_LOCAL_MACHINE,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(storeName))))
	if err != nil {
		return nil, fmt.Errorf("failed to open cert store: %w", err)
	}
	defer windows.CertCloseStore(st, 0)

	// prev is used for enumerating through all the certificates that matches the issuer.
	// On the first call to the function this parameter is NULL on all subsequent calls,
	// this parameter is the last CertContext pointer returned by the CertFindCertificateInStore function
	var prev *windows.CertContext

	// maxCertEnumeration would avoid requiring a infinite loop that relies on enumerating
	// until we get nil crt.
	for i := 1; i <= maxCertEnumeration; i++ {
		logger.Debugf("Attempt %d, searching certificate...", i)

		// https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certfindcertificateinstore
		crt, err := windows.CertFindCertificateInStore(
			st,
			windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING,
			0,
			windows.CERT_FIND_ISSUER_STR,
			unsafe.Pointer(syscall.StringToUTF16Ptr(issuer)),
			prev)

		if err != nil {
			return nil, fmt.Errorf("unable to find certificate: %w", err)
		}
		if crt == nil {
			return nil, fmt.Errorf("no certificate by issuer %s with ID %s", issuer, certID)
		}

		x509Cert, err := certContextToX509(crt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate context: %w", err)
		}

		if fmt.Sprintf("%x", x509Cert.SerialNumber) == certID {
			return crt, nil
		}

		prev = crt
	}

	return nil, nil
}

// writeClientCredentials stores client credentials (certificate and private key).
func (j *CredsJob) writeClientCredentials(creds []byte, outputFile string) error {
	num, err := serialNumber(outputFile)
	if err != nil {
		logger.Warningf("Could not get previous serial number, will skip cleanup: %v", err)
	}

	if err := utils.SaferWriteFile(creds, outputFile, 0644); err != nil {
		return fmt.Errorf("failed to write client key: %w", err)
	}

	pfx, err := generatePFX(creds)
	if err != nil {
		return fmt.Errorf("failed to generate PFX data from client credentials: %w", err)
	}

	p := filepath.Join(filepath.Dir(outputFile), pfxFile)
	if err := utils.SaferWriteFile(pfx, p, 0644); err != nil {
		return fmt.Errorf("failed to write PFX file: %w", err)
	}

	blob := windows.CryptDataBlob{
		Size: uint32(len(pfx)),
		Data: &pfx[0],
	}

	// https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-pfximportcertstore
	handle, err := windows.PFXImportCertStore(&blob, syscall.StringToUTF16Ptr(""), windows.CRYPT_MACHINE_KEYSET)
	if err != nil {
		return fmt.Errorf("failed to import PFX in cert store: %w", err)
	}
	defer windows.CertCloseStore(handle, 0)

	var crtCtx *windows.CertContext

	// https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certenumcertificatesinstore
	crtCtx, err = windows.CertEnumCertificatesInStore(handle, crtCtx)
	if err != nil {
		return fmt.Errorf("failed to get cert context for PFX from store: %w", err)
	}
	defer windows.CertFreeCertificateContext(crtCtx)

	// Add certificate to personal store.
	if err := addCtxToLocalSystemStore(my, crtCtx, uint32(windows.CERT_STORE_ADD_NEWER)); err != nil {
		return fmt.Errorf("failed to store pfx cert context: %w", err)
	}

	// Search for previous certificate if its not already in memory.
	if prevCtx == nil && num != "" {
		prevCtx, err = findCert(my, certificateIssuer, num)
		if err != nil {
			logger.Warningf("Failed to find previous certificate with error: %v", err)
		}
	}

	// Remove previous certificate only after successful refresh.
	if err := deleteCert(prevCtx, my); err != nil {
		logger.Warningf("Failed to delete previous certificate(%s) with error: %v", num, err)
	}

	prevCtx = windows.CertDuplicateCertificateContext(crtCtx)

	return nil
}

// certContextToX509 creates an x509 Certificate from a Windows cert context.
func certContextToX509(ctx *windows.CertContext) (*x509.Certificate, error) {
	der := unsafe.Slice(ctx.EncodedCert, int(ctx.Length))
	return x509.ParseCertificate(der)
}

// generatePFX accepts certificate concatenated with private key and generates a PFX out of it.
// https://learn.microsoft.com/en-us/windows-hardware/drivers/install/personal-information-exchange---pfx--files
func generatePFX(creds []byte) (pfxData []byte, err error) {
	cert, key := pem.Decode(creds)
	x509Cert, err := x509.ParseCertificate(cert.Bytes)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to parse client certificate: %w", err)
	}

	ecpvt, err := parsePvtKey(key)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to parse EC PrivateKey from client credentials: %w", err)
	}

	return pkcs12.Encode(rand.Reader, ecpvt, x509Cert, nil, "")
}

func addCtxToLocalSystemStore(storeName string, certContext *windows.CertContext, disposition uint32) error {
	// https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certopenstore
	// https://learn.microsoft.com/en-us/windows-hardware/drivers/install/local-machine-and-current-user-certificate-stores
	// https://learn.microsoft.com/en-us/windows/win32/seccrypto/system-store-locations#cert_system_store_local_machine
	st, err := windows.CertOpenStore(
		windows.CERT_STORE_PROV_SYSTEM,
		0,
		0,
		windows.CERT_SYSTEM_STORE_LOCAL_MACHINE,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(storeName))))
	if err != nil {
		return fmt.Errorf("failed to open cert store: %w", err)
	}
	// https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certclosestore
	defer windows.CertCloseStore(st, 0)

	// https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certaddcertificatecontexttostore
	if err := windows.CertAddCertificateContextToStore(st, certContext, disposition, nil); err != nil {
		return fmt.Errorf("failed to add certificate context to store: %w", err)
	}

	return nil
}

func deleteCert(crtCtx *windows.CertContext, storeName string) error {
	if crtCtx == nil {
		return nil
	}

	st, err := windows.CertOpenStore(
		windows.CERT_STORE_PROV_SYSTEM,
		0,
		0,
		windows.CERT_SYSTEM_STORE_LOCAL_MACHINE,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(storeName))))
	if err != nil {
		return fmt.Errorf("failed to open cert store: %w", err)
	}
	defer windows.CertCloseStore(st, 0)

	var dlCtx *windows.CertContext
	dlCtx, err = windows.CertFindCertificateInStore(
		st,
		windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING,
		0,
		windows.CERT_FIND_EXISTING,
		unsafe.Pointer(crtCtx),
		dlCtx,
	)
	if err != nil {
		return fmt.Errorf("unable to find the certificate in %q store to delete: %w", storeName, err)
	}

	return windows.CertDeleteCertificateFromStore(dlCtx)
}
