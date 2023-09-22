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

package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/ssh"
)

// MakeRandRSAPubKey generates base64 encoded 256 bit RSA public key for use in tests.
func MakeRandRSAPubKey(t *testing.T) string {
	t.Helper()
	prv, err := rsa.GenerateKey(rand.Reader, 256)
	if err != nil {
		t.Fatalf("error generating RSA key: %v", err)
	}
	sshPublic, err := ssh.NewPublicKey(prv.Public())
	if err != nil {
		t.Fatalf("error wrapping ssh public key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sshPublic.Marshal())
}
