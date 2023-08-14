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
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/google/tink/go/aead/subtle"
)

// parseCertificate validates certificate is in valid PEM format.
func parseCertificate(cert []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(cert)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM certificate")
	}

	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return x509Cert, nil
}

// parsePvtKey validates the key is in valid format and returns the EC Private Key.
func parsePvtKey(pemKey []byte) (*ecdsa.PrivateKey, error) {
	key, _ := pem.Decode(pemKey)
	if key == nil {
		return nil, fmt.Errorf("failed to decode PEM Key")
	}

	ecKey, err := x509.ParseECPrivateKey(key.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse EC Private Key: %w", err)
	}

	return ecKey, nil
}

// verifySign verifies the client certificate is valid and signed by root CA.
func verifySign(cert []byte, rootCAFile string) error {
	caCertPEM, err := os.ReadFile(rootCAFile)
	if err != nil {
		return fmt.Errorf("failed to read CA PEM file for verifying signature: %w", err)
	}

	x509Cert, err := parseCertificate(cert)
	if err != nil {
		return fmt.Errorf("failed to parse client certificate for verifying signature: %w", err)
	}

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caCertPEM)

	opts := x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	if _, err := x509Cert.Verify(opts); err != nil {
		return fmt.Errorf("failed to verify if client certificate against root CA %q: %w", rootCAFile, err)
	}

	return nil
}

// encrypt encrypts plain text using AES GCM algorithm.
func encrypt(aesKey []byte, plainText []byte, associatedData []byte) ([]byte, error) {
	cipher, err := subtle.NewAESGCM(aesKey)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cipher: %v", err)
	}
	return cipher.Encrypt(plainText, associatedData)
}

// decrypt decrypts AES GCM encrypted cipher text.
func decrypt(aesKey []byte, cipherText []byte, associatedData []byte) ([]byte, error) {
	cipher, err := subtle.NewAESGCM(aesKey)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cipher: %v", err)
	}
	return cipher.Decrypt(cipherText, associatedData)
}
