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
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/go-ini/ini"
)

func getDefaultAdapter(fes []ipForwardEntry) (*ipForwardEntry, error) {
	// Choose the first adapter index that has the default route setup.
	// This is equivalent to how route.exe works when interface is not provided.
	sort.Slice(fes, func(i, j int) bool { return fes[i].ipForwardIfIndex < fes[j].ipForwardIfIndex })
	for _, fe := range fes {
		if fe.ipForwardDest.Equal(net.ParseIP("0.0.0.0")) {
			return &fe, nil
		}
	}
	return nil, fmt.Errorf("could not find default route")
}

func addMetadataRoute() error {
	fes, err := getIPForwardEntries()
	if err != nil {
		return err
	}

	defaultRoute, err := getDefaultAdapter(fes)
	if err != nil {
		return err
	}

	forwardEntry := ipForwardEntry{
		ipForwardDest:    net.ParseIP("169.254.169.254"),
		ipForwardMask:    net.IPv4Mask(255, 255, 255, 255),
		ipForwardNextHop: net.ParseIP("0.0.0.0"),
		ipForwardMetric1: defaultRoute.ipForwardMetric1, // Must be <= the default route metric.
		ipForwardIfIndex: defaultRoute.ipForwardIfIndex,
	}

	for _, fe := range fes {
		if fe.ipForwardDest.Equal(forwardEntry.ipForwardDest) && fe.ipForwardIfIndex == forwardEntry.ipForwardIfIndex {
			// No need to add entry, it's already setup.
			return nil
		}
	}

	logger.Infof("Adding route to metadata server on adapter with index %d", defaultRoute.ipForwardIfIndex)
	return addIPForwardEntry(forwardEntry)
}

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
	config := cfg.Get()

	if runtime.GOOS == "windows" {
		// Indefinitely retry to set up required MDS route.
		for ; ; time.Sleep(1 * time.Second) {
			if err := addMetadataRoute(); err != nil {
				logger.Errorf("Could not set default route to metadata: %v", err)
			} else {
				break
			}
		}
	} else {
		// Linux instance setup.
		defer run.Quiet(ctx, "systemd-notify", "--ready")
		defer logger.Debugf("notify systemd")

		if config.Snapshots.Enabled {
			logger.Infof("Snapshot listener enabled")
			snapshotServiceIP := config.Snapshots.SnapshotServiceIP
			snapshotServicePort := config.Snapshots.SnapshotServicePort
			timeoutInSeconds := config.Snapshots.TimeoutInSeconds
			startSnapshotListener(ctx, snapshotServiceIP, snapshotServicePort, timeoutInSeconds)
		}

		scripts := []struct {
			enabled bool
			script  string
		}{
			{config.InstanceSetup.OptimizeLocalSSD, "optimize_local_ssd"},
			{config.InstanceSetup.SetMultiqueue, "set_multiqueue"},
		}

		// These scripts are run regardless of metadata/network access and config options.
		for _, curr := range scripts {
			if !curr.enabled {
				continue
			}

			if err := run.Quiet(ctx, "google_"+curr.script); err != nil {
				logger.Warningf("Failed to run %q script: %v", "google_"+curr.script, err)
			}
		}

		// Below actions happen on every agent start. They only need to
		// run once per boot, but it's harmless to run them on every
		// boot. If this changes, we will hook these to an explicit
		// on-boot signal.

		// Allow users to opt out of below instance setup actions.
		if !config.InstanceSetup.NetworkEnabled {
			logger.Infof("InstanceSetup.network_enabled is false, skipping setup actions that require metadata")
			return
		}

		// The below actions require metadata to be set, so if it
		// hasn't yet been set, wait on it here. In instances without
		// network access, this will become an indefinite wait.
		// TODO: split agentInit into needs-network and no-network functions.
		for newMetadata == nil {
			logger.Debugf("populate first time metadata...")
			newMetadata, _ = mdsClient.Get(ctx)
		}

		if !newMetadata.Instance.Attributes.DisableIOScheduler {
			logger.Debugf("set IO scheduler config")
			if err := setIOScheduler(); err != nil {
				logger.Warningf("Failed to set IO scheduler: %v", err)
			}
		}

		// Disable overcommit accounting; e2 instances only.
		parts := strings.Split(newMetadata.Instance.MachineType, "/")
		if strings.HasPrefix(parts[len(parts)-1], "e2-") {
			if err := run.Quiet(ctx, "sysctl", "vm.overcommit_memory=1"); err != nil {
				logger.Warningf("Failed to run 'sysctl vm.overcommit_memory=1': %v", err)
			}
		}

		// Check if instance ID has changed, and if so, consider this
		// the first boot of the instance.
		// TODO Also do this for windows. liamh@13-11-2019
		instanceIDFile := config.Instance.InstanceIDDir
		instanceID, err := os.ReadFile(instanceIDFile)
		if err != nil && !os.IsNotExist(err) {
			logger.Warningf("Not running first-boot actions, error reading instance ID: %v", err)
		} else {
			if string(instanceID) == "" {
				// If the file didn't exist or was empty, try legacy key from instance configs.
				instanceID = []byte(config.Instance.InstanceID)

				// Write instance ID to file for next time before moving on.
				towrite := fmt.Sprintf("%s\n", newMetadata.Instance.ID.String())
				if err := os.WriteFile(instanceIDFile, []byte(towrite), 0644); err != nil {
					logger.Warningf("Failed to write instance ID file: %v", err)
				}
			}
			if newMetadata.Instance.ID.String() != strings.TrimSpace(string(instanceID)) {
				logger.Infof("Instance ID changed, running first-boot actions")
				if config.InstanceSetup.SetHostKeys {
					if err := generateSSHKeys(ctx); err != nil {
						logger.Warningf("Failed to generate SSH keys: %v", err)
					}
				}
				if config.InstanceSetup.SetBotoConfig {
					if err := generateBotoConfig(); err != nil {
						logger.Warningf("Failed to create boto.cfg: %v", err)
					}
				}

				// Write instance ID to file.
				towrite := fmt.Sprintf("%s\n", newMetadata.Instance.ID.String())
				if err := os.WriteFile(instanceIDFile, []byte(towrite), 0644); err != nil {
					logger.Warningf("Failed to write instance ID file: %v", err)
				}
			}
		}
	}
}

func generateSSHKeys(ctx context.Context) error {
	config := cfg.Get()
	hostKeyDir := config.InstanceSetup.HostKeyDir
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
	configKeys := config.InstanceSetup.HostKeyTypes
	for _, keytype := range strings.Split(configKeys, ",") {
		keytypes[keytype] = true
	}

	// Generate new keys and upload to guest attributes.
	for keytype := range keytypes {
		keyfile := fmt.Sprintf("%s/ssh_host_%s_key", hostKeyDir, keytype)
		if err := run.Quiet(ctx, "ssh-keygen", "-t", keytype, "-f", keyfile+".temp", "-N", "", "-q"); err != nil {
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
		pubKey, err := os.ReadFile(keyfile + ".pub")
		if err != nil {
			logger.Errorf("Can't read %s public key: %v", keytype, err)
			continue
		}
		if vals := strings.Split(string(pubKey), " "); len(vals) >= 2 {
			if err := mdsClient.WriteGuestAttributes(ctx, "hostkeys/"+vals[0], vals[1]); err != nil {
				logger.Errorf("Failed to upload %s key to guest attributes: %v", keytype, err)
			}
		} else {
			logger.Warningf("Generated key is malformed, not uploading")
		}
	}

	_, err = exec.LookPath("restorecon")
	if err == nil {
		if err := run.Quiet(ctx, "restorecon", "-FR", hostKeyDir); err != nil {
			return fmt.Errorf("failed to restore SELinux context for: %s", hostKeyDir)
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
	botoCfg.Section("GSUtil").Key("default_api_version").SetValue("2")
	botoCfg.Section("GoogleCompute").Key("service_account").SetValue("default")

	return botoCfg.SaveTo(path)
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
