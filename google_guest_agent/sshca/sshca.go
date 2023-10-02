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

// Package sshca is the actual writing end of the sshtrustedca pipeline.
package sshca

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/sshtrustedca"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

// Certificates wrapps a list of certificate authorities.
type Certificates struct {
	Certs []TrustedCert `json:"trustedCertificateAuthorities"`
}

// TrustedCert defines the object containing a public key.
type TrustedCert struct {
	PublicKey string `json:"publicKey"`
}

var (
	// cachedCertificate stores the previously retrieved certificate to be cached in case mds fails.
	cachedCertificate string

	// mdsClient is the metadata's client, used to query oslogin certificates.
	mdsClient *metadata.Client
)

// Init initializes the sshca's event handler callback.
func Init(eventManager *events.Manager) {
	mdsClient = metadata.New()
	eventManager.Subscribe(sshtrustedca.ReadEvent, nil, writeFile)
}

// writeFile is an event handler callback and writes the actual sshca content to the pipe
// used by openssh to grant access based on ssh ca.
func writeFile(ctx context.Context, evType string, data interface{}, evData *events.EventData) bool {
	// There was some error on the pipe watcher, just ignore it.
	if evData.Error != nil {
		logger.Debugf("Not handling ssh trusted ca cert event, we got an error: %+v", evData.Error)
		return true
	}

	// Make sure we close the pipe after we've done writing to it.
	pipeData := evData.Data.(*sshtrustedca.PipeData)
	defer func() {
		if err := pipeData.File.Close(); err != nil {
			logger.Errorf("Failed to close pipe: %+v", err)
		}
		pipeData.Finished()
	}()

	// The certificates key/endpoint is not cached, we can't rely on the metadata watcher data because of that.
	certificate, err := mdsClient.GetKey(ctx, "oslogin/certificates", nil)
	if err != nil && cachedCertificate != "" {
		certificate = cachedCertificate
		logger.Warningf("Failed to get certificate, assuming/using previously cached one.")
	} else if err != nil {
		logger.Errorf("Failed to get certificate from metadata server: %+v", err)
		return true
	}

	// Keep a copy of the returned certificate for error fallback caching.
	cachedCertificate = certificate
	var certs Certificates
	var outData []string

	if err := json.Unmarshal([]byte(certificate), &certs); err != nil {
		logger.Errorf("Failed to unmarshal certificate json: %+v", err)
		return true
	}

	for _, curr := range certs.Certs {
		outData = append(outData, curr.PublicKey)
	}

	outStr := strings.Join(outData, "\n")
	n, err := pipeData.File.WriteString(outStr)
	if err != nil {
		logger.Errorf("Failed to write certificate to the write end of the pipe: %+v", err)
		return true
	}

	if n != len(outStr) {
		logger.Errorf("Wrote the wrong ammout of data, wrote %d bytes instead of %d bytes", n, len(certificate))
	}

	return true
}
