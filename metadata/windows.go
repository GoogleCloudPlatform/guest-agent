// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metadata

import (
	"encoding/json"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

// WindowsKey describes the WindowsKey metadata keys.
type WindowsKey struct {
	Email               string
	ExpireOn            string
	Exponent            string
	Modulus             string
	UserName            string
	HashFunction        string
	AddToAdministrators *bool
	PasswordLength      int
}

// WindowsKeys is a slice of WindowKey.
type WindowsKeys []WindowsKey

// UnmarshalJSON unmarshals b into WindowsKeys.
func (k *WindowsKeys) UnmarshalJSON(b []byte) error {
	var s string

	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	for _, jskey := range strings.Split(s, "\n") {
		var wk WindowsKey
		if err := json.Unmarshal([]byte(jskey), &wk); err != nil {
			logger.Errorf("failed to unmarshal windows key from metadata: %s", err)
			continue
		}

		expired, _ := utils.CheckExpired(wk.ExpireOn)
		if wk.Exponent != "" && wk.Modulus != "" && wk.UserName != "" && !expired {
			*k = append(*k, wk)
		}
	}

	return nil
}
