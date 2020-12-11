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
		expect                         string
	}{
		{0, 8, 1, "000000ff"},

		{0, 7, 4, "00000011"},
		{1, 7, 4, "00000022"},

		{0, 7, 2, "00000055"},
		{1, 7, 2, "0000002a"},

		{0, 8, 2, "00000055"},
		{1, 8, 2, "000000aa"},

		{0, 8, 3, "00000049"},
		{1, 8, 3, "00000092"},
		{2, 8, 3, "00000024"},

		{0, 8, 10, "00000001"},
		{1, 8, 10, "00000002"},
		{2, 8, 10, "00000004"},
		{8, 8, 10, "00000000"},

		{0, 32, 2, "55555555"},
		{1, 32, 2, "aaaaaaaa"},

		{0, 32, 3, "49249249"},
		{1, 32, 3, "92492492"},
		{2, 32, 3, "24924924"},

		{0, 33, 2, "00000001,55555555"},
		{1, 33, 2, "00000000,aaaaaaaa"},

		{0, 100, 2, "00000005,55555555,55555555,55555555"},
		{1, 100, 2, "0000000a,aaaaaaaa,aaaaaaaa,aaaaaaaa"},

		{0, 100, 32, "00000001,00000001,00000001,00000001"},
		{1, 100, 80, "00000000,00020000,00000000,00000002"},
	}
	for _, tt := range test {
		if actual := constructXPSString(tt.queueNum, tt.totalCPUs, tt.numQueues); actual != tt.expect {
			t.Errorf("constructXPSString(%d, %d, %d) incorrect return: actual %s, expect %s",
				tt.queueNum, tt.totalCPUs, tt.numQueues, actual, tt.expect)
		}
	}
}
