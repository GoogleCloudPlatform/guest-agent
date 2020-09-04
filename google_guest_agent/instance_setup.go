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
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/go-ini/ini"
)

const (
	instanceIDFile = "/etc/google_instance_id"
	virtioNetDevs  = "/sys/bus/virtio/drivers/virtio_net/virtio*"
	deviceRegex		 = "/e(\\w+)"
	queueRegex     = ".*tx-([0-9]+).*$"
	irqDirPath     = "/proc/irq/*"
	xpsCPU         = "/sys/class/net/e*/queues/tx*/xps_cpus"
	nvmeDevice     = "/sys/bus/pci/drivers/nvme/*"
	scsiDevice     = "/sys/bus/virtio/drivers/virtio_scsi/virtio*"
)

func agentInit(ctx context.Context) {
	// Actions to take on agent startup.
	//
	// On Windows:
	//  - Add route to metadata server
	// On Linux:
	//  - Generate SSH host keys (one time only).
	//  - Generate boto.cfg (one time only).
	//  - Set sysctl values.
	//  - Set scheduler values.
	//  - Optimize local ssd.
	//  - Set multiqueue.
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

		if config.Section("Snapshots").Key("enabled").MustBool(false) {
			logger.Infof("Snapshot listener enabled")
			snapshotServiceIP := config.Section("Snapshots").Key("snapshot_service_ip").MustString("169.254.169.254")
			snapshotServicePort := config.Section("Snapshots").Key("snapshot_service_port").MustInt(8081)
			startSnapshotListener(snapshotServiceIP, snapshotServicePort)
		}

		res := runCmdOutput(exec.Command("nproc"))
		if res.ExitCode() != 0 {
			logger.Warningf("Failed to run nproc: %v", res.Stderr())
			return
		}
		totalCPUs, err := strconv.Atoi(res.Stdout()[0 : len(res.Stdout())-1])
		if err != nil {
			logger.Warningf("Failed to get number of cpus: %v", err)
			return
		}
		// Config NVME, SCSI and set MultiQueue are run regardless of metadata/network access and config options.
		if err := configNVME(totalCPUs); err != nil {
			logger.Warningf("Failed to config nvme: %v", err)
		}

		if err := configSCSI(totalCPUs); err != nil {
			logger.Warningf("Failed to config scsi: %v", err)
		}

		/*
			For a single-queue / no MSI-X virtionet device, sets the IRQ affinities to
			processor 0. For this virtionet configuration, distributing IRQs to all
			processors results in comparatively high cpu utilization and comparatively
			low network bandwidth.

			For a multi-queue / MSI-X virtionet device, sets the IRQ affinities to the
			per-IRQ affinity hint. The virtionet driver maps each virtionet TX (RX) queue
			MSI-X interrupt to a unique single CPU if the number of TX (RX) queues equals
			the number of online CPUs. The mapping of network MSI-X interrupt vector to
			CPUs is stored in the virtionet MSI-X interrupt vector affinity hint. This
			configuration allows network traffic to be spread across the CPUs, giving
			each CPU a dedicated TX and RX network queue, while ensuring that all packets
			from a single flow are delivered to the same CPU.

			For a gvnic device, set the IRQ affinities to the per-IRQ affinity hint.
			The google virtual ethernet driver maps each queue MSI-X interrupt to a
			unique single CPU, which is stored in the affinity_hint for each MSI-X
			vector. In older versions of the kernel, irqblanace is expected to copy the
			affinity_hint to smp_affinity; however, GCE instances disable irqbalance by
			default. This script copies over the affinity_hint to smp_affinity on boot to
			replicate the behavior of irqbalance.
		*/
		if err := setMultiQueue(totalCPUs); err != nil {
			logger.Warningf("Failed to set multi queue: %v", err)
		}

		// Below actions happen on every agent start. They only need to
		// run once per boot, but it's harmless to run them on every
		// boot. If this changes, we will hook these to an explicit
		// on-boot signal.
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

func setMultiQueue(totalCPUs int) error {
	devices, err := filepath.Glob(virtioNetDevs)
	if err != nil {
		return err
	}
	for _, dev := range devices {
		if err := enableMultiQueue(dev); err != nil {
			logger.Warningf("Could not enable multi queue for %s.", dev)
			return err
		}
		if err = setQueueNumForDevice(dev); err != nil {
			logger.Warningf("Could not set queue num for %s.", dev)
			return err
		}
	}
	// Set smp_affinity properly for gvnic queues. '-ntfy-block.' is unique to gve and will not affect virtio queues.
	if err = setSMPAffinityForGVNIC(totalCPUs); err != nil {
		logger.Warningf("Could not set smp_affinity for gvnic.")
		return err
	}
	return nil
}

func setSMPAffinityForGVNIC(totalCPUs int) error {
	irqDirs, err := filepath.Glob(irqDirPath)
	if err != nil {
		return err
	}
	for _, irq := range irqDirs {
		blocks, err := filepath.Glob(irq + "/*-ntfy-block.*")
		if err != nil {
			return err
		}
		if len(blocks) > 0 && isFile(irq+"/affinity_hint") {
			if err = copyFile(irq+"/affinity_hint", irq+"/smp_affinity"); err != nil {
				return err
			}
		}
	}
	XPS, err := filepath.Glob(xpsCPU)
	if err != nil {
		return err
	}

	// If we have more CPUs than queues, then stripe CPUs across tx affinity as CPUNumber % queue_count.
	for _, q := range XPS {
		onlyOneQueue, err:= onlyOneCombinedQueue(q)
		if err != nil {
			return err
		}
		if onlyOneQueue {
			continue
		}

		numQueues := len(XPS)
		if numQueues > 63 {
			numQueues = 63
		}
		r, _ := regexp.Compile(queueRegex)
		match := r.MatchString(q)
		var queueNum int
		if match {
			queueNum, err = strconv.Atoi(r.FindAllStringSubmatch(q, -1)[0][1])
			if err != nil {
				return err
			}
		}
		xps := 0
		for _, cpu := range makeRange(queueNum, numQueues, totalCPUs-1) {
			xps |= 1 << cpu
		}
		// Linux xps_cpus requires a hex number with commas every 32 bits. It ignores
		// all bits above # cpus, so write a list of comma separated 32 bit hex values
		// with a comma between dwords.
		var xpsDwords []string
		for range makeRange(0, 1, (totalCPUs-1)/32) {
			xpsDwords = append(xpsDwords, fmt.Sprintf("%08x", xps&0xffffffff))
		}
		var xpsString = strings.Join(xpsDwords, ",")
		if err = ioutil.WriteFile(q, []byte(xpsString), 0644); err != nil {
			logger.Warningf("%v", err)
			return err
		}
		logger.Infof("Queue %d XPS=%s for %s\n", queueNum, xpsString, q)
	}
	return nil
}

func onlyOneCombinedQueue(q string) (bool, error) {
	r, _ := regexp.Compile(deviceRegex)
	match := r.MatchString(q)
	if match {
		// /eth0, /eth1
		ethDev := r.FindAllStringSubmatch(q, -1)[0][1]
		ethDev = ethDev[1:]
		combinedQueueNum, err := getCombinedQueueNum(ethDev)
		if err != nil {
			return false, err
		}
		if combinedQueueNum == 1 {
			return true, nil
		}
	}
	return false, nil
}

func makeRange(min, step, max int) []int {
	arr := make([]int, (max-min)/step+1)
	cur := min
	for i := range arr {
		arr[i] = cur
		cur += step
	}
	return arr
}

func copyFile(src string, dest string) error {
	input, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(dest, input, 0644)
	if err != nil {
		return err
	}
	return nil
}

func setQueueNumForDevice(dev string) error {
	dev = path.Base(dev)
	irqDirs, err := filepath.Glob(irqDirPath)
	if err != nil {
		return err
	}
	for _, irq := range irqDirs {
		smpAffinity := irq + "/smp_affinity_list"
		if isDir(smpAffinity) {
			continue
		}
		virtionetIntxDir := irq + "/" + dev
		virtionetMsixDirRegex := ".*/" + dev + "-(input|output)\\.([0-9]+)$"
		if isDir(virtionetIntxDir) {
			// All virtionet intx IRQs are delivered to CPU 0
			logger.Infof("Setting %s to 01 for device %s.", smpAffinity, dev)
			if err := ioutil.WriteFile(smpAffinity, []byte("01"), 0644); err != nil {
				return err
			}
			continue
		}
		// Not virtionet intx, probe for MSI-X
		virtionetMsixFound := 0
		irqDevs, err := filepath.Glob(irq + "/" + dev + "*")
		if err != nil {
			return err
		}
		var queueNum int
		for _, entry := range irqDevs {
			r, _ := regexp.Compile(virtionetMsixDirRegex)
			match := r.MatchString(entry)
			if match {
				virtionetMsixFound = 1
				// FindAllStringSubmatch return [][]string
				queueNum, err = strconv.Atoi(r.FindAllStringSubmatch(entry, -1)[0][2])
				if err != nil {
					return err
				}
			}
		}
		affinityHint := irq + "/affinity_hint"
		if virtionetMsixFound == 0 || isFile(affinityHint) {
			continue
		}

		//Set the IRQ CPU affinity to the virtionet-initialized affinity hint
		logger.Infof("Setting %s to %d for device %s.", smpAffinity, queueNum, dev)
		if err = ioutil.WriteFile(smpAffinity, []byte(strconv.Itoa(queueNum)), 0644); err != nil {
			return err
		}
	}
	return nil
}

func configNVME(totalCPUs int) error {
	var currentCPU = 0
	devices, err := filepath.Glob(nvmeDevice)
	if err != nil {
		return err
	}
	for _, dev := range devices {
		if !isDir(dev) {
			continue
		}
		irqs, err := filepath.Glob(dev + "/msi_irqs/*")
		if err != nil {
			return err
		}

		for _, irqInfo := range irqs {
			if !isFile(irqInfo) {
				continue
			}
			currentCPU := currentCPU % totalCPUs
			cpuMask := 1 << currentCPU
			irq := path.Base(irqInfo)
			logger.Infof("Setting IRQ %s smp_affinity to %d", irq, cpuMask)
			if err := ioutil.WriteFile("/proc/irq/"+irq+"/smp_affinity", []byte(strconv.Itoa(cpuMask)), 0644); err != nil {
				return err
			}
			currentCPU++
		}
	}
	return nil
}

func configSCSI(totalCPUs int) error {
	var irqs []int
	devices, err := filepath.Glob(scsiDevice)
	if err != nil {
		return err
	}
	for _, device := range devices {
		var ssd = 0
		targetPaths, err := filepath.Glob(device + "/host*/target*/*")
		if err != nil {
			return err
		}
		for _, targetPath := range targetPaths {
			if !isFile(targetPath + "/model") {
				continue
			}
			b, err := ioutil.ReadFile(targetPath + "/model")
			if err != nil {
				return err
			}
			match, err := regexp.MatchString(".*EphemeralDisk.* ", string(b))
			if err != nil {
				return err
			}
			if match {
				ssd = 1
				queuePaths, err := filepath.Glob(targetPath + "/block/sd*/queue")
				if err != nil {
					return err
				}
				for _, queuePath := range queuePaths {
					ioutil.WriteFile(queuePath+"/scheduler", []byte("noop"), 0644)
					ioutil.WriteFile(queuePath+"/add_random", []byte("0"), 0644)
					ioutil.WriteFile(queuePath+"/nr_requests", []byte("512"), 0644)
					ioutil.WriteFile(queuePath+"/rotational", []byte("0"), 0644)
					ioutil.WriteFile(queuePath+"/rq_affinity", []byte("0"), 0644)
					ioutil.WriteFile(queuePath+"/nomerges", []byte("1"), 0644)
				}
			}
		}
		if ssd == 1 {
			irq, err := getIRQFromInterrupts(path.Base(device) + "-request")
			if err != nil {
				return err
			}
			irqs = append(irqs, irq)
		}
	}
	irqCount := len(irqs)
	if irqCount != 0 {
		stride := totalCPUs / irqCount
		if stride < 1 {
			stride = 1
		}
		currentCPU := 0
		for _, irq := range irqs {
			currentCPU %= totalCPUs
			cpuMask := 1 << currentCPU
			logger.Infof("Setting IRQ %d smp_affinity to %d", irq, cpuMask)
			err := ioutil.WriteFile(fmt.Sprintf("/proc/irq/%d/smp_affinity", irq), []byte(strconv.Itoa(cpuMask)), 0644)
			if err != nil {
				return err
			}
			currentCPU += stride
		}
	}
	return nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func getIRQFromInterrupts(requestQueue string) (int, error) {
	var irq int
	f, err := os.Open("/proc/interrupts")
	if err != nil {
		return 0, err
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, requestQueue) {
			irq, err = strconv.Atoi(strings.Split(line, ":")[0])
			if err != nil {
				return 0, err
			}
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	return irq, nil
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
			f, err := os.OpenFile("/sys/block/"+dev+"/queue/scheduler", os.O_WRONLY|os.O_TRUNC, 0644)
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
