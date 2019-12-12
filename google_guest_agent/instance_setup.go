//  Copyright 2019 Google Inc. All Rights Reserved.
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
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/go-ini/ini"
)

func forwardEntryExists(fes []ipForwardEntry, fe ipForwardEntry) bool {
	for _, e := range fes {
		if e.ipForwardIfIndex == fe.ipForwardIfIndex && e.ipForwardDest.Equal(fe.ipForwardDest) {
			return true
		}
	}
	return false
}

func agentInit() error {
	// Actions to take on agent startup.
	//
	// On Windows:
	//  - Add route to metadata server
	// On Linux:
	//  - Generate SSH host keys (one time only).
	//  - Generate boto.cfg (one time only).
	//  - Set sysctl values.
	//  - Set scheduler values.
	//  - Run `google_optimize_local_ssd` script.
	//  - Run `google_set_multiqueue` script.
	// TODO incorporate these scripts into the agent. liamh@12-11-19
	if runtime.GOOS == "windows" {
		fes, err := getIPForwardEntries()
		if err != nil {
			return err
		}

		interfaces, err := net.Interfaces()
		if err != nil {
			return err
		}

		// We only want to set this for the first adapter, pre Windows 10 (2016) this was not guaranteed to be in metric order.
		sort.SliceStable(interfaces, func(i, j int) bool {
			return interfaces[i].Index < interfaces[j].Index
		})

		for _, iface := range interfaces {
			// Only take action on interfaces that are up, are not Loopback, and are not vEtherent (commonly setup by docker).
			if strings.Contains(iface.Name, "vEthernet") || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}

			fe := ipForwardEntry{
				ipForwardDest:    net.ParseIP("169.254.169.254"),
				ipForwardMask:    net.IPv4Mask(255, 255, 255, 255),
				ipForwardNextHop: net.ParseIP("0.0.0.0"),
				ipForwardMetric1: 1,
				ipForwardIfIndex: int32(iface.Index),
			}

			if forwardEntryExists(fes, fe) {
				break
			}

			logger.Infof("Adding route to metadata server on %q (index: %d)", iface.Name, iface.Index)
			if err := addIPForwardEntry(fe); err != nil {
				logger.Errorf("Error adding route to metadata server on %q (index: %d): %v", iface.Name, iface.Index, err)
			}
			break
		}
	} else {
		// Check if instance ID has changed, and if so, consider this
		// the first boot of the instance.
		// TODO Also do this for windows. liamh@13-11-2019
		instanceID, err := ioutil.ReadFile("/etc/google_instance_id")
		if err != nil && !os.IsNotExist(err) {
			logger.Warningf("Not running first-boot actions, error reading instance ID: %v", err)
		} else {
			if string(instanceID) == "" {
				// If the file didn't exist or was empty, try legacy key from instance configs.
				instanceID = []byte(config.Section("Instance").Key("instance_id").String())
				if err := ioutil.WriteFile("/etc/google_instance_id", []byte(newMetadata.Instance.ID.String()), 0644); err != nil {
					logger.Warningf("Failed to write instance ID file: %v", err)
				}
			}
			if newMetadata.Instance.ID.String() != string(instanceID) {
				logger.Infof("Instance ID changed, running first-boot actions")
				if err := generateSSHKeys(); err != nil {
					logger.Warningf("Failed to generate SSH keys: %v", err)
				}
				if err := generateBotoConfig(); err != nil {
					logger.Warningf("Failed to create boto.cfg: %v", err)
				}
				if err := ioutil.WriteFile("/etc/google_instance_id", []byte(newMetadata.Instance.ID.String()), 0644); err != nil {
					logger.Warningf("Failed to write instance ID file: %v", err)
				}
			}
		}

		// Below actions happen on every agent start. They only need to
		// run once per boot, but it's harmless to run them on every
		// boot. If this changes, we will hook these to an explicit
		// on-boot signal.
		if err = setIOScheduler(); err != nil {
			logger.Warningf("Failed to set IO scheduler: %v", err)
		}

		// Disable overcommit accounting; e2 instances only.
		parts := strings.Split(newMetadata.Instance.MachineType, "/")
		if strings.HasPrefix(parts[len(parts)-1], "e2-") {
			if err := runCmd(exec.Command("sysctl", "vm.overcommit_memory=1")); err != nil {
				logger.Warningf("Failed to run 'sysctl vm.overcommit_memory=1': %v", err)
			}
		}

		for _, script := range []string{"google_optimize_local_ssd", "google_set_multiqueue"} {
			if err := runCmd(exec.Command(script)); err != nil {
				logger.Warningf("Failed to run %q script: %v", script, err)
			}
		}
	}
	return nil
}

func generateSSHKeys() error {
	dir, err := os.Open("/etc/ssh")
	if err != nil {
		return err
	}
	defer dir.Close()

	files, err := dir.Readdirnames(0)
	if err != nil {
		return err
	}

	keytypes := make(map[string]bool)

	// Find keys present on disk, and deduce their type from filename.
	prefix := "ssh_host_"
	suffix := "_key"
	for _, file := range files {
		if strings.HasPrefix(file, prefix) && strings.HasSuffix(file, suffix) && len(file) > len(prefix+suffix) {
			keytype := file
			keytype = strings.TrimPrefix(keytype, prefix)
			keytype = strings.TrimSuffix(keytype, suffix)
			keytypes[keytype] = true
		}
	}

	// List keys we should generate, according to the config.
	configKeys := config.Section("InstanceSetup").Key("host_key_types").MustString("ecdsa,ed25519,rsa")
	for _, keytype := range strings.Split(configKeys, ",") {
		keytypes[keytype] = true
	}

	// Generate new keys and upload to guest attributes.
	for keytype, _ := range keytypes {
		keyfile := fmt.Sprintf("/etc/ssh/ssh_host_%s_key", keytype)
		if err := runCmd(exec.Command("ssh-keygen", "-t", keytype, "-f", keyfile+".temp", "-N", "", "-q")); err != nil {
			logger.Warningf("Failed to generate SSH host key %q: %v", keyfile, err)
			continue
		}
		if err := os.Chmod(keyfile+".temp", 0600); err != nil {
			logger.Errorf("Failed to chmod SSH host key %q: %v", keyfile, err)
			continue
		}
		if err := os.Chmod(keyfile+".temp.pub", 0644); err != nil {
			logger.Errorf("Failed to chmod SSH host key %q: %v", keyfile+".pub", err)
			continue
		}
		if err := os.Rename(keyfile+".temp", keyfile); err != nil {
			logger.Errorf("Failed to overwrite %q: %v", keyfile, err)
			continue
		}
		if err := os.Rename(keyfile+".temp.pub", keyfile+".pub"); err != nil {
			logger.Errorf("Failed to overwrite %q: %v", keyfile+".pub", err)
			continue
		}
		pubKey, err := ioutil.ReadFile(keyfile + ".pub")
		if err != nil {
			logger.Errorf("Can't read %s public key: %v", keytype, err)
			continue
		}
		if vals := strings.Split(string(pubKey), " "); len(vals) >= 2 {
			if err := writeGuestAttributes("hostkeys/"+vals[0], vals[1]); err != nil {
				logger.Errorf("Failed to upload %s key to guest attributes: %v", keytype, err)
			}
		} else {
			logger.Warningf("Generated key is malformed, not uploading")
		}
	}
	return nil
}

func generateBotoConfig() error {
	path := "/etc/boto.cfg"
	botoCfg, err := ini.LooseLoad(path, path+".template")
	if err != nil {
		return err
	}
	botoCfg.Section("GSUtil").Key("default_project_id").SetValue(newMetadata.Project.NumericProjectID.String())
	botoCfg.Section("GSUtil").Key("default_apt_version").SetValue("2")
	botoCfg.Section("GoogleCompute").Key("service_account").SetValue("default")

	return botoCfg.SaveTo(path)
}

func writeGuestAttributes(key, value string) error {
	client := &http.Client{Timeout: defaultTimeout}
	finalURL := metadataURL + "instance/guest-attributes/" + key
	req, err := http.NewRequest("PUT", finalURL, strings.NewReader(value))
	if err != nil {
		return err
	}
	req.Header.Add("Metadata-Flavor", "Google")

	_, err = client.Do(req)
	return err
}

func setIOScheduler() error {
	dir, err := os.Open("/sys/block")
	if err != nil {
		return err
	}
	defer dir.Close()

	devs, err := dir.Readdirnames(0)
	if err != nil {
		return err
	}

	for _, dev := range devs {
		// Detect if device is using MQ subsystem.
		stat, err := os.Stat("/sys/block/" + dev + "/mq")
		if err == nil && stat.IsDir() {
			f, err := os.OpenFile("/sys/block/"+dev+"/queue/scheduler", os.O_WRONLY|os.O_TRUNC, 0700)
			if err != nil {
				return err
			}
			_, err = f.Write([]byte("none"))
			if err != nil {
				return err
			}
		}
	}
	return nil
}
