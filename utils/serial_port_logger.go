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

// Serial port logger util for Google Guest Agent, Google Metadata script runner and Google Authorized Keys

package utils

import "go.bug.st/serial"

// SerialPort is a type for writing to a named serial port.
type SerialPort struct {
	Port string
}

func (s *SerialPort) Write(b []byte) (int, error) {
	p, err := serial.Open(s.Port, &serial.Mode{BaudRate: 115200})
	if err != nil {
		return 0, err
	}
	defer p.Close()

	return p.Write(b)
}
