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

const (
	// defaultCredsDir is the directory location for MTLS MDS credentials.
	defaultCredsDir = "/run/google-mds-mtls"
)

var (
	// certUpdaters is a map of known CA certificate updaters with the local directory paths for certificates.
	certUpdaters = map[string][]string{
		// SUSE, Debian and Ubuntu distributions.
		// https://manpages.ubuntu.com/manpages/xenial/man8/update-ca-certificates.8.html
		// https://github.com/openSUSE/ca-certificates
		"update-ca-certificates": {"/usr/local/share/ca-certificates", "/usr/share/pki/trust/anchors"},
		// CentOS, Fedora, RedHat distributions.
		// https://www.unix.com/man-page/centos/8/UPDATE-CA-TRUST
		"update-ca-trust": {"/etc/pki/ca-trust/source/anchors"},
	}
)
