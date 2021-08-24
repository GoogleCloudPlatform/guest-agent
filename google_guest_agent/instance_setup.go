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
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/go-ini/ini"
)

func agentInit(ctx context.Context) {
	// Actions to take on agent startup.
	//
	// All platforms:
	//  - Determine if metadata hostname can be resolved.
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
		msg := "Could not set default route to metadata"
		fes, err := getIPForwardEntries()
		if err != nil {
			logger.Errorf("%s, error listing IPForwardEntries: %v", msg, err)
			return
		}

		// Choose the first adapter index that has the default route setup.
		// This is equivalent to how route.exe works when interface is not provided.
		var index int32
		var found bool
		var metric int32
		sort.Slice(fes, func(i, j int) bool { return fes[i].ipForwardIfIndex < fes[j].ipForwardIfIndex })
		for _, fe := range fes {
			if fe.ipForwardDest.Equal(net.ParseIP("0.0.0.0")) {
				index = fe.ipForwardIfIndex
				metric = fe.ipForwardMetric1
				found = true
				break
			}
		}

		if found == false {
			logger.Errorf("%s, could not find the default route in IPForwardEntries: %+v", msg, fes)
			return
		}

		iface, err := net.InterfaceByIndex(int(index))
		if err != nil {
			logger.Errorf("%s, error from net.InterfaceByIndex(%d): %v", msg, index, err)
			return
		}

		forwardEntry := ipForwardEntry{
			ipForwardDest:    net.ParseIP("169.254.169.254"),
			ipForwardMask:    net.IPv4Mask(255, 255, 255, 255),
			ipForwardNextHop: net.ParseIP("0.0.0.0"),
			ipForwardMetric1: metric, // This needs to be at least equal to the default route metric.
			ipForwardIfIndex: int32(iface.Index),
		}

		for _, fe := range fes {
			if fe.ipForwardDest.Equal(forwardEntry.ipForwardDest) && fe.ipForwardIfIndex == forwardEntry.ipForwardIfIndex {
				// No need to add entry, it's already setup.
				return
			}
		}

		logger.Infof("Adding route to metadata server on %q (index: %d)", iface.Name, iface.Index)
		if err := addIPForwardEntry(forwardEntry); err != nil {
			logger.Errorf("%s, error adding IPForwardEntry on %q (index: %d): %v", msg, iface.Name, iface.Index, err)
			return
		}
	} else {
		// Linux instance setup.
		defer runCmd(exec.Command("systemd-notify", "--ready"))
		defer logger.Debugf("notify systemd")

		if config.Section("Snapshots").Key("enabled").MustBool(false) {
			logger.Infof("Snapshot listener enabled")
			snapshotServiceIP := config.Section("Snapshots").Key("snapshot_service_ip").MustString("169.254.169.254")
			snapshotServicePort := config.Section("Snapshots").Key("snapshot_service_port").MustInt(8081)
			startSnapshotListener(snapshotServiceIP, snapshotServicePort)
		}

		// These scripts are run regardless of metadata/network access and config options.
		for _, script := range []string{"optimize_local_ssd", "set_multiqueue"} {
			if config.Section("InstanceSetup").Key(script).MustBool(true) {
				if err := runCmd(exec.Command("google_" + script)); err != nil {
					logger.Warningf("Failed to run %q script: %v", "google_"+script, err)
				}
			}
		}

		// Below actions happen on every agent start. They only need to
		// run once per boot, but it's harmless to run them on every
		// boot. If this changes, we will hook these to an explicit
		// on-boot signal.

		logger.Debugf("set IO scheduler config")
		if err := setIOScheduler(); err != nil {
			logger.Warningf("Failed to set IO scheduler: %v", err)
		}

		// Allow users to opt out of below instance setup actions.
		if !config.Section("InstanceSetup").Key("network_enabled").MustBool(true) {
			logger.Infof("InstanceSetup.network_enabled is false, skipping setup actions that require metadata")
			return
		}

		// The below actions require metadata to be set, so if it
		// hasn't yet been set, wait on it here. In instances without
		// network access, this will become an indefinite wait.
		// TODO: split agentInit into needs-network and no-network functions.
		for newMetadata == nil {
			logger.Debugf("populate first time metadata...")
			newMetadata, _ = getMetadata(ctx, false)
			time.Sleep(1 * time.Second)
		}

		// Disable overcommit accounting; e2 instances only.
		parts := strings.Split(newMetadata.Instance.MachineType, "/")
		if strings.HasPrefix(parts[len(parts)-1], "e2-") {
			if err := runCmd(exec.Command("sysctl", "vm.overcommit_memory=1")); err != nil {
				logger.Warningf("Failed to run 'sysctl vm.overcommit_memory=1': %v", err)
			}
		}

		// Check if instance ID has changed, and if so, consider this
		// the first boot of the instance.
		// TODO Also do this for windows. liamh@13-11-2019
		instanceIDFile := config.Section("Instance").Key("instance_id_dir").MustString("/etc") + "/google_instance_id"
		instanceID, err := ioutil.ReadFile(instanceIDFile)
		if err != nil && !os.IsNotExist(err) {
			logger.Warningf("Not running first-boot actions, error reading instance ID: %v", err)
		} else {
			if string(instanceID) == "" {
				// If the file didn't exist or was empty, try legacy key from instance configs.
				instanceID = []byte(config.Section("Instance").Key("instance_id").String())

				// Write instance ID to file for next time before moving on.
				towrite := fmt.Sprintf("%s\n", newMetadata.Instance.ID.String())
				if err := ioutil.WriteFile(instanceIDFile, []byte(towrite), 0644); err != nil {
					logger.Warningf("Failed to write instance ID file: %v", err)
				}
			}
			if newMetadata.Instance.ID.String() != strings.TrimSpace(string(instanceID)) {
				logger.Infof("Instance ID changed, running first-boot actions")
				if config.Section("InstanceSetup").Key("set_host_keys").MustBool(true) {
					if err := generateSSHKeys(); err != nil {
						logger.Warningf("Failed to generate SSH keys: %v", err)
					}
				}
				if config.Section("InstanceSetup").Key("set_boto_config").MustBool(true) {
					if err := generateBotoConfig(); err != nil {
						logger.Warningf("Failed to create boto.cfg: %v", err)
					}
				}

				// Write instance ID to file.
				towrite := fmt.Sprintf("%s\n", newMetadata.Instance.ID.String())
				if err := ioutil.WriteFile(instanceIDFile, []byte(towrite), 0644); err != nil {
					logger.Warningf("Failed to write instance ID file: %v", err)
				}
			}
		}

	}
}

func generateSSHKeys() error {
	hostKeyDir := config.Section("InstanceSetup").Key("host_key_dir").MustString("/etc/ssh")
	dir, err := os.Open(hostKeyDir)
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
	for keytype := range keytypes {
		keyfile := fmt.Sprintf("%s/ssh_host_%s_key", hostKeyDir, keytype)
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
	runCmd(exec.Command("restorecon", "-FR", hostKeyDir))
	return nil
}

func generateBotoConfig() error {
	path := "/etc/boto.cfg"
	botoCfg, err := ini.LooseLoad(path, path+".template")
	if err != nil {
		return err
	}
	botoCfg.Section("GSUtil").Key("default_project_id").SetValue(newMetadata.Project.NumericProjectID.String())
	botoCfg.Section("GSUtil").Key("default_api_version").SetValue("2")
	botoCfg.Section("GoogleCompute").Key("service_account").SetValue("default")

	return botoCfg.SaveTo(path)
}

func writeGuestAttributes(key, value string) error {
	logger.Debugf("write guest attribute %q", key)
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
