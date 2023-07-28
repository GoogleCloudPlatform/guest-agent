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
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/google/tink/go/aead/subtle"
)

// verifyCertificate validates certificate is in valid PEM format.
func verifyCertificate(cert []byte) error {
	block, _ := pem.Decode(cert)
	if block == nil {
		return fmt.Errorf("failed to parse PEM certificate")
	}

	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
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
