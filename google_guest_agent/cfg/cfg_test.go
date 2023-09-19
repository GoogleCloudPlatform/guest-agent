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

import "testing"

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
