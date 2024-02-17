// Copyright 2024 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"fmt"
	"reflect"
	"regexp"
	"testing"
)

func TestRegexGroupsMap(t *testing.T) {
	tests := []struct {
		exp      string
		data     string
		expected map[string]string
	}{
		{
			"/(?P<dir>.*)/(?P<file>.*)",
			"/foo/bar",
			map[string]string{"dir": "foo", "file": "bar"},
		},
		{
			`(?P<name>.*)\.(?P<extension>.*)`,
			"foo.bar",
			map[string]string{"name": "foo", "extension": "bar"},
		},
		{
			`(?P<name>.*)\.(?P<extension>.*)`,
			"foobar",
			map[string]string{},
		},
	}

	for i, curr := range tests {
		t.Run(fmt.Sprintf("regex-groups-map-success-%d", i), func(t *testing.T) {
			compRegex := regexp.MustCompile(curr.exp)
			mp := RegexGroupsMap(compRegex, curr.data)
			if !reflect.DeepEqual(mp, curr.expected) {
				t.Fatalf("RegexGroupsMap() expected: %+v, got: %+v", curr.expected, mp)
			}
		})
	}
}
