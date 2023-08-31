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

import "github.com/GoogleCloudPlatform/guest-agent/utils"

const (
	// defaultCredsDir is the directory location for MTLS MDS credentials.
	defaultCredsDir = "/run/google-mds-mtls"
	// rootCACertFileName is the root CA cert.
	rootCACertFileName = "root.crt"
	// clientCredsFileName are client credentials, its basically the file
	// that has the EC private key and the client certificate concatenated.
	clientCredsFileName = "client.key"
)

// writeRootCACert writes Root CA cert from UEFI variable to output file.
func (j *CredsJob) writeRootCACert(content []byte, outputFile string) error {
	return utils.SaferWriteFile(content, outputFile)
}

// writeClientCredentials stores client credentials (certificate and private key).
func (j *CredsJob) writeClientCredentials(plaintext []byte, outputFile string) error {
	return utils.SaferWriteFile(plaintext, outputFile)
}
