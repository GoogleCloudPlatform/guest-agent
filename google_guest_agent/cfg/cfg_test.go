//  Copyright 2023 Google Inc. All Rights Reserved.
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

package cfg

import (
	"os"
	"path"
	"testing"
)

func TestLoad(t *testing.T) {
	if err := Load(nil); err != nil {
		t.Fatalf("Failed to load configuration: %+v", err)
	}

	cfg := Get()
	if cfg.WSFC != nil {
		t.Errorf("WSFC shouldn't not be defined by default configuration, expected: nil, got: non-nil")
	}

	if cfg.Accounts.DeprovisionRemove == true {
		t.Errorf("Expected Accounts.deprovision_remove to be: false, got: true")
	}
}

func TestInvalidConfig(t *testing.T) {
	invalidConfig := `
[Section
key = value
`

	dataSources = func(extraDefaults []byte) []interface{} {
		return []interface{}{
			[]byte(invalidConfig),
		}
	}

	// After testing set it back to the default one.
	defer func() {
		dataSources = defaultDataSources
	}()

	if err := Load(nil); err == nil {
		t.Errorf("Load() didn't fail to load invalid configuration, expected error")
	}
}

func TestDefaultDataSources(t *testing.T) {
	expectedDataSources := 4
	sources := defaultDataSources(nil)
	if len(sources) != expectedDataSources {
		t.Errorf("defaultDataSources() returned wrong number of sources, expected: %d, got: %d",
			expectedDataSources, len(sources))
	}

	_, ok := sources[0].([]byte)
	if !ok {
		t.Errorf("defaultDataSources() returned wrong sources, first source should be of type []byte")
	}
}

func TestDefaultConfigFile(t *testing.T) {
	windowsConfig := `C:\Program Files\Google\Compute Engine\instance_configs.cfg`
	unixConfig := `/etc/default/instance_configs.cfg`

	if got := defaultConfigFile("windows"); got != windowsConfig {
		t.Errorf("defaultConfigFile(windows) returned wrong file, expected: %s, got: %s", windowsConfig, got)
	}

	if got := defaultConfigFile("linux"); got != unixConfig {
		t.Errorf("defaultConfigFile(linux) returned wrong file, expected: %s, got: %s", unixConfig, got)
	}
}

func TestGetTwice(t *testing.T) {
	if err := Load(nil); err != nil {
		t.Fatalf("Failed to load configuration: %+v", err)
	}

	firstCfg := Get()
	secondCfg := Get()

	if firstCfg != secondCfg {
		t.Errorf("Get() should return always the same pointer, expected: %p, got: %p", firstCfg, secondCfg)
	}
}

func TestConfigLoadOrder(t *testing.T) {
	config := path.Join(t.TempDir(), "config.cfg")
	configFile = func(string) string { return config }
	t.Cleanup(func() { configFile = defaultConfigFile })
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
			// If this fails, other test case results are not valid
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
			err := os.WriteFile(config+".distro", []byte(tc.distroConfig), 0777)
			if err != nil {
				t.Fatal(err)
			}
			err = os.WriteFile(config+".template", []byte(tc.templateConfig), 0777)
			if err != nil {
				t.Fatal(err)
			}
			err = os.WriteFile(config, []byte(tc.userConfig), 0777)
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
