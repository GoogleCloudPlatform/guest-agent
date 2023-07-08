package metadata

import (
	"encoding/json"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	badKeys   []string
	badExpire []string
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

// Expired check if a given expired key is valid or not.
func Expired(expireOn string) bool {
	expired, err := utils.CheckExpired(expireOn)
	if err != nil {
		if !utils.ContainsString(expireOn, badExpire) {
			logger.Errorf("error parsing time: %s", err)
			badExpire = append(badExpire, expireOn)
		}
		return true
	}
	return expired
}

// UnmarshalJSON unmarshals b into WindowsKeys.
func (k *WindowsKeys) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	for _, jskey := range strings.Split(s, "\n") {
		var wk WindowsKey
		if err := json.Unmarshal([]byte(jskey), &wk); err != nil {
			if !utils.ContainsString(jskey, badKeys) {
				logger.Errorf("failed to unmarshal windows key from metadata: %s", err)
				badKeys = append(badKeys, jskey)
			}
			continue
		}
		if wk.Exponent != "" && wk.Modulus != "" && wk.UserName != "" && !Expired(wk.ExpireOn) {
			*k = append(*k, wk)
		}
	}
	return nil
}
