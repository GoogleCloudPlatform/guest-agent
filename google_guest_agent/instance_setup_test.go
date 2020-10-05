//  Copyright 2020 Google Inc. All Rights Reserved.
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

package main

import (
	"testing"
)

func TestConstructXPSString(t *testing.T) {
	test := []struct {
		queueNum, totalCPUs, numQueues int
		expect string
	}{
		{0, 8,2,
			"00000055"},
		{1, 8,2,
		"000000aa"},
		{0, 33,2,
			"55555555,00000001"},
		{0, 33,33,
			"00000001,00000000"},
	}
	for _, tt := range test {
		if actual := constructXPSString(tt.queueNum, tt.totalCPUs, tt.numQueues); actual != tt.expect {
			t.Errorf("constructXPSString(%d, %d, %d) incorrect return: actual %s, expect %s",
				tt.queueNum, tt.totalCPUs, tt.numQueues, actual, tt.expect)
		}
	}
}