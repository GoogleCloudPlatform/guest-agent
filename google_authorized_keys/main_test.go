// Copyright 2022 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
)

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func boolToStr(b *bool) string {
	if b == nil {
		return "<nil>"
	}
	return strconv.FormatBool(*b)
}

var t = true
var f = false
var truebool *bool = &t
var falsebool *bool = &f

func TestParseSSHKeys(t *testing.T) {
	pubKeyA := utils.MakeRandRSAPubKey(t)
	pubKeyB := utils.MakeRandRSAPubKey(t)
	pubKey := utils.MakeRandRSAPubKey(t)

	keys := []string{
		"# Here is some random data in the file.",
		fmt.Sprintf("usera:ssh-rsa %s", pubKeyA),
		fmt.Sprintf("userb:ssh-rsa %s", pubKeyB),
		fmt.Sprintf(`usera:ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, pubKey),
		fmt.Sprintf(`usera:ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2020-04-23T12:34:56+0000"}`, pubKey),
	}
	expected := []string{
		fmt.Sprintf("ssh-rsa %s", pubKeyA),
		fmt.Sprintf(`ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, pubKey),
	}

	user := "usera"

	if got, want := parseSSHKeys(user, keys), expected; !stringSliceEqual(got, want) {
		t.Errorf("ParseSSHKeys(%s,%s) incorrect return: got %v, want %v", user, keys, got, want)
	}

}

func TestCheckWinSSHEnabled(t *testing.T) {
	tests := []struct {
		instanceEnable *bool
		projectEnable  *bool
		expected       bool
	}{
		{
			instanceEnable: truebool,
			projectEnable:  nil,
			expected:       true,
		},
		{
			instanceEnable: falsebool,
			projectEnable:  nil,
			expected:       false,
		},
		{
			instanceEnable: falsebool,
			projectEnable:  truebool,
			expected:       false,
		},
		{
			instanceEnable: nil,
			projectEnable:  truebool,
			expected:       true,
		},
		{
			instanceEnable: nil,
			projectEnable:  falsebool,
			expected:       false,
		},
		{
			instanceEnable: nil,
			projectEnable:  nil,
			expected:       false,
		},
	}
	for _, tt := range tests {
		instanceAttributes := attributes{EnableWindowsSSH: tt.instanceEnable}
		projectAttributes := attributes{EnableWindowsSSH: tt.projectEnable}
		if got, want := checkWinSSHEnabled(&instanceAttributes, &projectAttributes), tt.expected; got != want {
			t.Errorf("checkWinSSHEnabled(%s, %s) incorrect return: got %v, want %v", boolToStr(tt.instanceEnable), boolToStr(tt.projectEnable), got, want)
		}
	}
}

func TestGetUserKeysNew(t *testing.T) {
	pubKey := utils.MakeRandRSAPubKey(t)

	tests := []struct {
		userName         string
		instanceMetadata attributes
		projectMetadata  attributes
		expectedKeys     []string
	}{
		{
			userName: "name",
			instanceMetadata: attributes{
				BlockProjectSSHKeys: false,
				SSHKeys: []string{
					fmt.Sprintf("name:ssh-rsa %s instance1", pubKey),
					fmt.Sprintf("othername:ssh-rsa %s instance2", pubKey),
				},
			},
			projectMetadata: attributes{
				SSHKeys: []string{
					fmt.Sprintf("name:ssh-rsa %s project1", pubKey),
					fmt.Sprintf("othername:ssh-rsa %s project2", pubKey),
				},
			},
			expectedKeys: []string{
				fmt.Sprintf("ssh-rsa %s instance1", pubKey),
				fmt.Sprintf("ssh-rsa %s project1", pubKey),
			},
		},
		{
			userName: "name",
			instanceMetadata: attributes{
				BlockProjectSSHKeys: true,
				SSHKeys: []string{
					fmt.Sprintf("name:ssh-rsa %s instance1", pubKey),
					fmt.Sprintf("othername:ssh-rsa %s instance2", pubKey),
				},
			},
			projectMetadata: attributes{
				SSHKeys: []string{
					fmt.Sprintf("name:ssh-rsa %s project1", pubKey),
					fmt.Sprintf("othername:ssh-rsa %s project2", pubKey),
				},
			},
			expectedKeys: []string{fmt.Sprintf("ssh-rsa %s instance1", pubKey)},
		},
		{
			userName: "name",
			instanceMetadata: attributes{
				BlockProjectSSHKeys: false,
				SSHKeys: []string{
					fmt.Sprintf("name:ssh-rsa %s instance1", pubKey),
					fmt.Sprintf("othername:ssh-rsa %s instance2", pubKey),
				},
			},
			projectMetadata: attributes{
				SSHKeys: nil,
			},
			expectedKeys: []string{fmt.Sprintf("ssh-rsa %s instance1", pubKey)},
		},
		{
			userName: "name",
			instanceMetadata: attributes{
				BlockProjectSSHKeys: false,
				SSHKeys:             nil,
			},
			projectMetadata: attributes{
				SSHKeys: []string{
					fmt.Sprintf("name:ssh-rsa %s project1", pubKey),
					fmt.Sprintf("othername:ssh-rsa %s project2", pubKey),
				},
			},
			expectedKeys: []string{fmt.Sprintf("ssh-rsa %s project1", pubKey)},
		},
	}

	for count, tt := range tests {
		t.Run(fmt.Sprintf("test-%d", count), func(t *testing.T) {
			if got, want := getUserKeys(tt.userName, &tt.instanceMetadata, &tt.projectMetadata), tt.expectedKeys; !stringSliceEqual(got, want) {
				t.Errorf("getUserKeys[%d] incorrect return: got %v, want %v", count, got, want)
			}
		})
	}
}

func TestGetMetadataAttributes(t *testing.T) {
	tests := []struct {
		att       *attributes
		expectErr bool
	}{
		{
			att:       &attributes{EnableWindowsSSH: truebool, SSHKeys: []string{"name:ssh-rsa [KEY] instance1", "othername:ssh-rsa [KEY] instance2"}, BlockProjectSSHKeys: false},
			expectErr: false,
		},
		{
			att:       &attributes{EnableWindowsSSH: truebool, SSHKeys: []string{"name:ssh-rsa [KEY] instance1", "othername:ssh-rsa [KEY] instance2"}, BlockProjectSSHKeys: true},
			expectErr: false,
		},
		{
			att:       &attributes{EnableWindowsSSH: nil, SSHKeys: []string{"name:ssh-rsa [KEY] instance1", "othername:ssh-rsa [KEY] instance2"}, BlockProjectSSHKeys: false},
			expectErr: false,
		},
		{
			att:       &attributes{EnableWindowsSSH: falsebool, SSHKeys: []string{"name:ssh-rsa [KEY] instance1", "othername:ssh-rsa [KEY] instance2"}, BlockProjectSSHKeys: false},
			expectErr: false,
		},
		{
			att:       nil,
			expectErr: true,
		},
	}

	client = &mdsClient{}

	for count, tt := range tests {
		want := tt.att
		hasErr := false
		reqStr := fmt.Sprintf("/attributes/%d", count)
		got, err := getMetadataAttributes(context.Background(), reqStr)
		if err != nil {
			hasErr = true
		}

		if !reflect.DeepEqual(got, want) || hasErr != tt.expectErr {
			t.Errorf("Failed: Got: %v, Want: %v, Error: %v", got, want, err)
		}
	}
}

type mdsClient struct{}

func (mds *mdsClient) Get(ctx context.Context) (*metadata.Descriptor, error) {
	return nil, fmt.Errorf("Get() not yet implemented")
}

func (mds *mdsClient) GetKey(ctx context.Context, key string, headers map[string]string) (string, error) {
	return "", fmt.Errorf("GetKey() not yet implemented")
}

func (mds *mdsClient) GetKeyRecursive(ctx context.Context, key string) (string, error) {
	i, err := strconv.Atoi(key[strings.LastIndex(key, "/")+1:])
	if err != nil {
		return "", err
	}

	switch i {
	case 0:
		return `{"enable-windows-ssh":"true","ssh-keys":"name:ssh-rsa [KEY] instance1\nothername:ssh-rsa [KEY] instance2","block-project-ssh-keys":"false","other-metadata":"foo"}`, nil
	case 1:
		return `{"enable-windows-ssh":"true","ssh-keys":"name:ssh-rsa [KEY] instance1\nothername:ssh-rsa [KEY] instance2","block-project-ssh-keys":"true","other-metadata":"foo"}`, nil
	case 2:
		return `{"ssh-keys":"name:ssh-rsa [KEY] instance1\nothername:ssh-rsa [KEY] instance2","block-project-ssh-keys":"false","other-metadata":"foo"}`, nil
	case 3:
		return `{"enable-windows-ssh":"false","ssh-keys":"name:ssh-rsa [KEY] instance1\nothername:ssh-rsa [KEY] instance2","other-metadata":"foo"}`, nil
	case 4:
		return "BADJSON", nil
	default:
		return "", fmt.Errorf("unknown key %q", key)
	}
}

func (mds *mdsClient) Watch(ctx context.Context) (*metadata.Descriptor, error) {
	return nil, fmt.Errorf("Watch() not yet implemented")
}

func (mds *mdsClient) WriteGuestAttributes(ctx context.Context, key string, value string) error {
	return fmt.Errorf("WriteGuestattributes() not yet implemented")
}
