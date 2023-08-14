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
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"golang.org/x/sys/windows"
	"software.sslmate.com/src/go-pkcs12"
)

const (
	// defaultCredsDir is the directory location for MTLS MDS credentials.
	defaultCredsDir = `C:\Program Files\Google\Compute Engine\certs\mds`
	// pfxFile stores client credentials in PFX format.
	pfxFile = "client.key.pfx"
	// cryptExportable (CRYPT_EXPORTABLE) is used to mark imported as keys as exportable.
	cryptExportable = 0x00000001
	// https://learn.microsoft.com/en-us/windows/win32/seccrypto/system-store-locations
	// my is predefined personal cert store.
	my = "MY"
	// root is predefined cert store for root trusted CA certs.
	root = "ROOT"
)

var (
	prevCtx *windows.CertContext
)

// writeRootCACert writes Root CA cert from UEFI variable to output file.
func (j *CredsJob) writeRootCACert(cacert []byte, outputFile string) error {
	if err := utils.WriteFile(cacert, outputFile); err != nil {
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

	return nil
}

// writeClientCredentials stores client credentials (certificate and private key).
func (j *CredsJob) writeClientCredentials(creds []byte, outputFile string) error {
	if err := utils.WriteFile(creds, outputFile); err != nil {
		return fmt.Errorf("failed to write client key: %w", err)
	}

	pfx, err := generatePFX(creds)
	if err != nil {
		return fmt.Errorf("failed to generate PFX data from client credentials: %w", err)
	}

	p := filepath.Join(filepath.Dir(outputFile), pfxFile)
	if err := utils.WriteFile(pfx, p); err != nil {
		return fmt.Errorf("failed to write PFX file: %w", err)
	}

	blob := windows.CryptDataBlob{
		Size: uint32(len(pfx)),
		Data: &pfx[0],
	}

	// https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-pfximportcertstore
	handle, err := windows.PFXImportCertStore(&blob, syscall.StringToUTF16Ptr(""), uint32(cryptExportable))
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

	// Best effort to cleanup previous certificates and keep only the latest.
	// When the agent restarts, it will not have the previous context in memory.
	// Therefore, it will not remove any old certificates that are not required.
	// If there are multiple similar certificates, clients should try to use the one
	// with the longest expiry date, as that will be the latest version of it.
	// TODO: See if we can have a more robust process for cleaning up old certificates,
	// while ensuring that we don't delete any that were not installed by the agent process.
	if err := deleteCert(prevCtx, my); err != nil {
		logger.Warningf("Failed to delete previous certificate with error: %v", err)
	}

	prevCtx = windows.CertDuplicateCertificateContext(crtCtx)

	return nil
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
