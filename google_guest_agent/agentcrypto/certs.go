package agentcrypto

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// VerifyCertificate validates certificate is in valid PEM format.
func VerifyCertificate(cert []byte) error {
	block, _ := pem.Decode(cert)
	if block == nil {
		return fmt.Errorf("failed to parse PEM certificate")
	}

	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	return nil
}
