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
