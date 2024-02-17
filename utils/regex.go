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

// Regexp util for Google Guest Agent.

package utils

import "regexp"

// RegexGroupsMap takes a compiled Regexp and a string data and return a map
// of groups:data pairs. The compiled regex must contain grouping.
func RegexGroupsMap(regex *regexp.Regexp, data string) map[string]string {
	match := regex.FindStringSubmatch(data)

	groups := make(map[string]string)
	for i, name := range regex.SubexpNames() {
		if i > 0 && i <= len(match) {
			groups[name] = match[i]
		}
	}

	return groups
}
