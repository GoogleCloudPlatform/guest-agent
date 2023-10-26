//  Copyright 2023 Google LLC
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

//go:build integration
// +build integration

package cfg

import (
	"os"
	"runtime"
	"testing"
)

func TestConfigDefault(t *testing.T) {
	configFile := defaultConfigFile(runtime.GOOS)
	testcases := []struct {
		name           string
		extraDefault   string
		distroConfig   string
		templateConfig string
		userConfig     string
		output         bool
	}{
		{
			name:           "user config override",
			extraDefault:   "[NetworkInterfaces]\nSetup = true\n",
			distroConfig:   "[NetworkInterfaces]\nSetup = true\n",
			templateConfig: "[NetworkInterfaces]\nSetup = true\n",
			userConfig:     "[NetworkInterfaces]\nSetup = false\n",
			output:         false,
		},
		{
			name:           "template config override",
			extraDefault:   "[NetworkInterfaces]\nSetup = true\n",
			distroConfig:   "[NetworkInterfaces]\nSetup = true\n",
			templateConfig: "[NetworkInterfaces]\nSetup = false\n",
			userConfig:     "",
			output:         false,
		},
		{
			name:           "distro config override",
			extraDefault:   "[NetworkInterfaces]\nSetup = true\n",
			distroConfig:   "[NetworkInterfaces]\nSetup = false\n",
			templateConfig: "",
			userConfig:     "",
			output:         false,
		},
		{
			name:           "extra default override",
			extraDefault:   "[NetworkInterfaces]\nSetup = false\n",
			distroConfig:   "",
			templateConfig: "",
			userConfig:     "",
			output:         false,
		},
		{
			// If this fails, integration test results are not valid
			name:           "default is true",
			extraDefault:   "",
			distroConfig:   "",
			templateConfig: "",
			userConfig:     "",
			output:         true,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := os.WriteFile(configFile+".distro", []byte(tc.distroConfig), 0777)
			if err != nil {
				t.Fatal(err)
			}
			err = os.WriteFile(configFile+".template", []byte(tc.templateConfig), 0777)
			if err != nil {
				t.Fatal(err)
			}
			err = os.WriteFile(configFile, []byte(tc.userConfig), 0777)
			if err != nil {
				t.Fatal(err)
			}
			err = Load([]byte(tc.extraDefault))
			if err != nil {
				t.Fatal(err)
			}
			if Get().NetworkInterfaces.Setup != tc.output {
				t.Errorf("unexpected config value for NetworkInterfaces.Setup, wanted %v but got %v", Get().NetworkInterfaces.Setup, tc.output)
			}
		})
	}
}
