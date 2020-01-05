//  Copyright 2017 Google Inc. All Rights Reserved.
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

// GCEMetadataScripts handles the running of metadata scripts on Google Compute
// Engine instances.
package main

// TODO: compare log outputs in this utility to linux. incorporate config from guest-agent.
// TODO: standardize and consolidate retries.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	programName    = "GCEMetadataScripts"
	version        = "dev"
	metadataURL    = "http://metadata.google.internal/computeMetadata/v1"
	metadataHang   = "/?recursive=true&alt=json&timeout_sec=10&last_etag=NONE"
	defaultTimeout = 20 * time.Second
	powerShellArgs = []string{"-NoProfile", "-NoLogo", "-ExecutionPolicy", "Unrestricted", "-File"}
	usageError     = fmt.Errorf("No valid arguments specified. Specify one of \"startup\", \"shutdown\" or \"specialize\"")

	storageURL = "storage.googleapis.com"

	bucket = `([a-z0-9][-_.a-z0-9]*)`
	object = `(.+)`

	// Many of the Google Storage URLs are supported below.
	// It is preferred that customers specify their object using
	// its gs://<bucket>/<object> URL.
	bucketRegex = regexp.MustCompile(fmt.Sprintf(`^gs://%s/?$`, bucket))
	gsRegex     = regexp.MustCompile(fmt.Sprintf(`^gs://%s/%s$`, bucket, object))

	// Check for the Google Storage URLs:
	// http://<bucket>.storage.googleapis.com/<object>
	// https://<bucket>.storage.googleapis.com/<object>
	gsHTTPRegex1 = regexp.MustCompile(fmt.Sprintf(`^http[s]?://%s\.storage\.googleapis\.com/%s$`, bucket, object))

	// http://storage.cloud.google.com/<bucket>/<object>
	// https://storage.cloud.google.com/<bucket>/<object>
	gsHTTPRegex2 = regexp.MustCompile(fmt.Sprintf(`^http[s]?://storage\.cloud\.google\.com/%s/%s$`, bucket, object))

	// Check for the other possible Google Storage URLs:
	// http://storage.googleapis.com/<bucket>/<object>
	// https://storage.googleapis.com/<bucket>/<object>
	//
	// The following are deprecated but also checked:
	// http://commondatastorage.googleapis.com/<bucket>/<object>
	// https://commondatastorage.googleapis.com/<bucket>/<object>
	gsHTTPRegex3 = regexp.MustCompile(fmt.Sprintf(`^http[s]?://(?:commondata)?storage\.googleapis\.com/%s/%s$`, bucket, object))

	testStorageClient *storage.Client
)

func newStorageClient(ctx context.Context) (*storage.Client, error) {
	if testStorageClient != nil {
		return testStorageClient, nil
	}
	return storage.NewClient(ctx)
}

func downloadGSURL(ctx context.Context, bucket, object string, file *os.File) error {
	client, err := newStorageClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create storage client: %v", err)
	}
	defer client.Close()

	r, err := client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("error reading object %q: %v", object, err)
	}
	defer r.Close()

	_, err = io.Copy(file, r)
	return err
}

func downloadURL(url string, file *os.File) error {
	// Retry up to 3 times, only wait 1 second between retries.
	var res *http.Response
	var err error
	for i := 1; ; i++ {
		res, err = http.Get(url)
		if err != nil && i > 3 {
			return err
		}
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %q, bad status: %s", url, res.Status)
	}

	_, err = io.Copy(file, res.Body)
	return err
}

func downloadScript(ctx context.Context, path string, file *os.File) error {
	// Startup scripts may run before DNS is running on some systems,
	// particularly once a system is promoted to a domain controller.
	// Try to lookup storage.googleapis.com and sleep for up to 100s if
	// we get an error.
	// TODO: do we need to do this on every script?
	for i := 0; i < 20; i++ {
		if _, err := net.LookupHost(storageURL); err == nil {
			break
		}
		time.Sleep(5 * time.Second)
	}
	bucket, object := parseGCS(path)
	if bucket != "" && object != "" {
		// TODO: why is this retry outer, but downloadURL retry is inner?
		// Retry up to 3 times, only wait 1 second between retries.
		for i := 1; ; i++ {
			err := downloadGSURL(ctx, bucket, object, file)
			if err == nil {
				return nil
			}
			if err != nil && i > 3 {
				logger.Infof("Failed to download GCS path: %v", err)
				break
			}
			time.Sleep(1 * time.Second)
		}
		logger.Infof("Trying unauthenticated download")
		path = fmt.Sprintf("https://%s/%s/%s", storageURL, bucket, object)
	}

	// Fall back to an HTTP GET of the URL.
	return downloadURL(path, file)
}

func parseGCS(path string) (string, string) {
	for _, re := range []*regexp.Regexp{gsRegex, gsHTTPRegex1, gsHTTPRegex2, gsHTTPRegex3} {
		match := re.FindStringSubmatch(path)
		if len(match) == 3 {
			return match[1], match[2]
		}
	}
	return "", ""
}

func getMetadataKey(key string) (string, error) {
	md, err := getMetadata(key, false)
	if err != nil {
		return "", err
	}
	return string(md), nil
}

func getMetadataAttributes(key string) (map[string]string, error) {
	md, err := getMetadata(key, true)
	if err != nil {
		return nil, err
	}
	var att map[string]string
	return att, json.Unmarshal(md, &att)
}

func getMetadata(key string, recurse bool) ([]byte, error) {
	client := &http.Client{
		Timeout: defaultTimeout,
	}

	url := metadataURL + key
	if recurse {
		url += metadataHang
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Metadata-Flavor", "Google")

	var res *http.Response
	// Retry forever, increase sleep between retries (up to 5 times) in order
	// to wait for slow network initialization.
	var rt time.Duration
	for i := 1; ; i++ {
		res, err = client.Do(req)
		if err == nil {
			break
		}
		if i < 6 {
			rt = time.Duration(3*i) * time.Second
		}
		logger.Errorf("error connecting to metadata server, retrying in %s, error: %v", rt, err)
		time.Sleep(rt)
	}
	defer res.Body.Close()

	md, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return md, nil
}

// runScript makes a temporary directory and temporary file for the script, downloads and then runs it.
func runScript(ctx context.Context, key, value string) error {
	var u *url.URL
	if strings.HasSuffix(key, "-url") {
		var err error
		u, err = url.Parse(strings.TrimSpace(value))
		if err != nil {
			return err
		}
	}

	// Make temp directory.
	// dir, err := ioutil.TempDir(config.Section("MetadataScripts").Key("run_dir"), "metadata-scripts")
	dir, err := ioutil.TempDir("", "metadata-scripts")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	// These extensions need to be present on Windows. Doesn't hurt to add
	// on other systems though.
	tmpFile := filepath.Join(dir, key)
	for _, ext := range []string{"bat", "cmd", "ps1"} {
		switch {
		case strings.HasSuffix(key, fmt.Sprintf("-%s", ext)),
			u != nil && strings.HasSuffix(u.Path, fmt.Sprintf(".%s", ext)):
			tmpFile = fmt.Sprintf("%s.%s", tmpFile, ext)
			break
		}
	}

	// Create or download files.
	if u != nil {
		file, err := os.OpenFile(tmpFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("error opening temp file: %v", err)
		}
		if err := downloadScript(ctx, value, file); err != nil {
			file.Close()
			return err
		}
		file.Close()
	} else {
		if err := ioutil.WriteFile(tmpFile, []byte(value), 0755); err != nil {
			return err
		}
	}

	// Craft the command to run.
	var c *exec.Cmd
	if strings.HasSuffix(tmpFile, ".ps1") {
		c = exec.Command("powershell.exe", append(powerShellArgs, tmpFile)...)
	} else {
		if runtime.GOOS == "windows" {
			c = exec.Command(tmpFile)
		} else {
			//c = exec.Command(config.Section("MetadataScripts").Key("default_shell").MustString("/bin/bash"), "-c", tmpFile)
			c = exec.Command("/bin/bash", "-c", tmpFile)
		}
	}

	return runCmd(c, key)
}

func runCmd(c *exec.Cmd, name string) error {
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	defer pr.Close()

	c.Stdout = pw
	c.Stderr = pw

	if err := c.Start(); err != nil {
		return err
	}
	pw.Close()

	in := bufio.NewScanner(pr)
	for in.Scan() {
		logger.Log(logger.LogEntry{
			Message:   fmt.Sprintf("%s: %s", name, in.Text()),
			CallDepth: 3,
			Severity:  logger.Info,
		})
	}

	return c.Wait()
}

// getWantedKeys returns the list of keys to check for a given type of script and OS.
func getWantedKeys(args []string, os string) ([]string, error) {
	if len(args) != 2 {
		return nil, usageError
	}
	prefix := args[1]
	switch prefix {
	case "specialize":
		prefix = "sysprep-specialize"
	case "startup", "shutdown":
		if os == "windows" {
			prefix = "windows-" + prefix
		}
		// if !config.Section("MetadataScripts").Key(prefix).MustBool(true) {
		// 	return nil, fmt.Errorf("%s scripts disabled in instance config.", prefix)
		// }
	default:
		return nil, usageError
	}

	var mdkeys []string
	suffixes := []string{"url"}
	if os == "windows" {
		// This ordering matters. URL is last on Windows, first otherwise.
		suffixes = []string{"ps1", "cmd", "bat", "url"}
	}

	for _, suffix := range suffixes {
		mdkeys = append(mdkeys, fmt.Sprintf("%s-script-%s", prefix, suffix))
	}

	// The 'bare' startup-script or shutdown-script key, not supported on Windows.
	if os != "windows" {
		mdkeys = append(mdkeys, fmt.Sprintf("%s-script", prefix))
	}

	return mdkeys, nil
}

func parseMetadata(md map[string]string, wanted []string) map[string]string {
	found := make(map[string]string)
	for _, key := range wanted {
		val, ok := md[key]
		if !ok || val == "" {
			continue
		}
		found[key] = val
	}
	return found
}

// getExistingKeys returns the wanted keys that are set in metadata.
func getExistingKeys(wanted []string) (map[string]string, error) {
	for _, attrs := range []string{"/instance/attributes", "/project/attributes"} {
		md, err := getMetadataAttributes(attrs)
		if err != nil {
			return nil, err
		}
		if found := parseMetadata(md, wanted); len(found) != 0 {
			return found, nil
		}
	}
	return nil, nil
}

func logFormat(e logger.LogEntry) string {
	now := time.Now().Format("2006/01/02 15:04:05")
	return fmt.Sprintf("%s %s: %s", now, programName, e.Message)
}

func main() {
	ctx := context.Background()
	opts := logger.LogOpts{
		LoggerName:     programName,
		FormatFunction: logFormat,
		Writers:        []io.Writer{os.Stdout},
	}

	// The keys to check vary based on the argument and the OS. Also functions to validate arguments.
	wantedKeys, err := getWantedKeys(os.Args, runtime.GOOS)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		os.Exit(2)
	}

	projectID, err := getMetadataKey("/project/project-id")
	if err == nil {
		opts.ProjectName = projectID
	} else {
		// TODO: just consider it disabled if no project is set..
		opts.DisableCloudLogging = true
	}
	logger.Init(ctx, opts)

	logger.Infof("Starting %s scripts (version %s).", os.Args[1], version)

	scripts, err := getExistingKeys(wantedKeys)
	if err != nil {
		logger.Fatalf(err.Error())
	}

	if len(scripts) == 0 {
		logger.Infof("No %s scripts to run.", os.Args[1])
		return
	}

	for key, value := range scripts {
		logger.Infof("Found %s in metadata.", key)
		if err := runScript(ctx, key, value); err != nil {
			logger.Infof("%s %s", key, err)
			continue
		}
		logger.Infof("%s exit status 0", key)
	}

	logger.Infof("Finished running %s scripts.", os.Args[1])
}
