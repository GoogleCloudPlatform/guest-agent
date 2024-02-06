// Copyright 2017 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// GCEMetadataScripts handles the running of metadata scripts on Google Compute
// Engine instances.
package main

// TODO: compare log outputs in this utility to linux.
// TODO: standardize and consolidate retries.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	storageURL     = "storage.googleapis.com"
	bucket         = "([a-z0-9][-_.a-z0-9]*)"
	object         = "(.+)"
	defaultTimeout = 20 * time.Second
)

var (
	programName    = path.Base(os.Args[0])
	powerShellArgs = []string{"-NoProfile", "-NoLogo", "-ExecutionPolicy", "Unrestricted", "-File"}
	errUsage       = fmt.Errorf("no valid arguments specified. Specify one of \"startup\", \"shutdown\" or \"specialize\"")

	// Many of the Google Storage URLs are supported below.
	// It is preferred that customers specify their object using
	// its gs://<bucket>/<object> URL.
	gsRegex = regexp.MustCompile(fmt.Sprintf(`^gs://%s/%s$`, bucket, object))

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

	client  metadata.MDSClientInterface
	version string
)

func init() {
	client = metadata.New()
}

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

func getMetadataKey(ctx context.Context, key string) (string, error) {
	md, err := getMetadata(ctx, key, false)
	if err != nil {
		return "", err
	}
	return string(md), nil
}

func getMetadataAttributes(ctx context.Context, key string) (map[string]string, error) {
	md, err := getMetadata(ctx, key, true)
	if err != nil {
		return nil, err
	}
	var att map[string]string
	return att, json.Unmarshal(md, &att)
}

func getMetadata(ctx context.Context, key string, recurse bool) ([]byte, error) {
	var resp string
	var err error

	if recurse {
		resp, err = client.GetKeyRecursive(ctx, key)
	} else {
		resp, err = client.GetKey(ctx, key, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("unable to get %q from MDS, with recursive flag set to %t: %w", key, recurse, err)
	}

	return []byte(resp), nil
}

func normalizeFilePathForWindows(filePath string, metadataKey string, gcsScriptURL *url.URL) string {
	// If either the metadataKey ends in one of these extensions OR if this is a url startup script and if the
	// url path ends in one of these extensions, append the extension to the filePath name so that Windows can recognize it.
	for _, ext := range []string{"bat", "cmd", "ps1", "exe"} {
		if strings.HasSuffix(metadataKey, "-"+ext) || (gcsScriptURL != nil && strings.HasSuffix(gcsScriptURL.Path, "."+ext)) {
			filePath = fmt.Sprintf("%s.%s", filePath, ext)
			break
		}
	}
	return filePath
}

func writeScriptToFile(ctx context.Context, value string, filePath string, gcsScriptURL *url.URL) error {
	// Create or download files.
	if gcsScriptURL != nil {
		file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("error opening temp file: %v", err)
		}
		if err := downloadScript(ctx, value, file); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("error closing temp file: %v", err)
		}
	} else {
		// Trim leading spaces and newlines.
		value = strings.TrimLeft(value, " \n\v\f\t\r")
		if err := os.WriteFile(filePath, []byte(value), 0755); err != nil {
			return fmt.Errorf("error writing temp file: %v", err)
		}
	}

	return nil
}

func setupAndRunScript(ctx context.Context, metadataKey string, value string) error {
	// Make sure that the URL is valid for URL startup scripts
	var gcsScriptURL *url.URL
	if strings.HasSuffix(metadataKey, "-url") {
		var err error
		gcsScriptURL, err = url.Parse(strings.TrimSpace(value))
		if err != nil {
			return err
		}
	}

	// Make temp directory.
	tmpDir, err := os.MkdirTemp(cfg.Get().MetadataScripts.RunDir, "metadata-scripts")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, metadataKey)
	if runtime.GOOS == "windows" {
		tmpFile = normalizeFilePathForWindows(tmpFile, metadataKey, gcsScriptURL)
	}

	if err := writeScriptToFile(ctx, value, tmpFile, gcsScriptURL); err != nil {
		return fmt.Errorf("unable to write script to file: %v", err)
	}

	return runScript(tmpFile, metadataKey)
}

// Craft the command to run.
func runScript(filePath string, metadataKey string) error {
	var cmd *exec.Cmd
	if strings.HasSuffix(filePath, ".ps1") {
		cmd = exec.Command("powershell.exe", append(powerShellArgs, filePath)...)
	} else {
		if runtime.GOOS == "windows" {
			cmd = exec.Command(filePath)
		} else {
			cmd = exec.Command(cfg.Get().MetadataScripts.DefaultShell, "-c", filePath)
		}
	}
	return runCmd(cmd, metadataKey)
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
	for {
		if !in.Scan() {
			if err := in.Err(); err != nil {
				logger.Errorf("error while communicating with %q script: %v", name, err)
			}
			break
		}
		logger.Log(logger.LogEntry{
			Message:   fmt.Sprintf("%s: %s", name, in.Text()),
			CallDepth: 3,
			Severity:  logger.Info,
		})
	}
	pr.Close()

	return c.Wait()
}

// getWantedKeys returns the list of keys to check for a given type of script and OS.
func getWantedKeys(args []string, os string) ([]string, error) {
	if len(args) != 2 {
		return nil, errUsage
	}
	prefix := args[1]
	switch prefix {
	case "specialize":
		prefix = "sysprep-specialize"
	case "startup":
		if os == "windows" {
			prefix = "windows-" + prefix
			if !cfg.Get().MetadataScripts.StartupWindows {
				return nil, fmt.Errorf("windows startup scripts disabled in instance config")
			}
		} else {
			if !cfg.Get().MetadataScripts.Startup {
				return nil, fmt.Errorf("startup scripts disabled in instance config")
			}
		}
	case "shutdown":
		if os == "windows" {
			prefix = "windows-" + prefix
			if !cfg.Get().MetadataScripts.ShutdownWindows {
				return nil, fmt.Errorf("windows shutdown scripts disabled in instance config")
			}
		} else {
			if !cfg.Get().MetadataScripts.Shutdown {
				return nil, fmt.Errorf("shutdown scripts disabled in instance config")
			}
		}
	default:
		return nil, errUsage
	}

	var mdkeys []string
	var suffixes []string
	if os == "windows" {
		suffixes = []string{"ps1", "cmd", "bat", "url"}
	} else {
		suffixes = []string{"url"}
		// The 'bare' startup-script or shutdown-script key, not supported on Windows.
		mdkeys = append(mdkeys, fmt.Sprintf("%s-script", prefix))
	}

	for _, suffix := range suffixes {
		mdkeys = append(mdkeys, fmt.Sprintf("%s-script-%s", prefix, suffix))
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
func getExistingKeys(ctx context.Context, wanted []string) (map[string]string, error) {
	for _, attrs := range []string{"/instance/attributes", "/project/attributes"} {
		md, err := getMetadataAttributes(ctx, attrs)
		if err != nil {
			return nil, err
		}
		if found := parseMetadata(md, wanted); len(found) != 0 {
			return found, nil
		}
	}
	return nil, nil
}

func logFormatWindows(e logger.LogEntry) string {
	now := time.Now().Format("2006/01/02 15:04:05")
	// 2006/01/02 15:04:05 GCEMetadataScripts This is a log message.
	return fmt.Sprintf("%s %s: %s", now, programName, e.Message)
}

func main() {
	ctx := context.Background()

	opts := logger.LogOpts{LoggerName: programName}

	if runtime.GOOS == "windows" {
		opts.Writers = []io.Writer{&utils.SerialPort{Port: "COM1"}, os.Stdout}
		opts.FormatFunction = logFormatWindows
	} else {
		opts.Writers = []io.Writer{os.Stdout}
		opts.FormatFunction = func(e logger.LogEntry) string { return e.Message }
		// Local logging is syslog; we will just use stdout in Linux.
		opts.DisableLocalLogging = true
	}

	var err error
	if err := cfg.Load(nil); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load instance configuration: %+v", err)
		os.Exit(1)
	}

	// The keys to check vary based on the argument and the OS. Also functions to validate arguments.
	wantedKeys, err := getWantedKeys(os.Args, runtime.GOOS)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		os.Exit(2)
	}

	projectID, err := getMetadataKey(ctx, "/project/project-id")
	if err == nil {
		opts.ProjectName = projectID
	}

	logger.Init(ctx, opts)

	logger.Infof("Starting %s scripts (version %s).", os.Args[1], version)

	scripts, err := getExistingKeys(ctx, wantedKeys)
	if err != nil {
		logger.Fatalf(err.Error())
	}

	if len(scripts) == 0 {
		logger.Infof("No %s scripts to run.", os.Args[1])
		return
	}

	for _, wantedKey := range wantedKeys {
		value, ok := scripts[wantedKey]
		if !ok {
			continue
		}
		logger.Infof("Found %s in metadata.", wantedKey)
		if err := setupAndRunScript(ctx, wantedKey, value); err != nil {
			logger.Warningf("Script %q failed with error: %v", wantedKey, err)
			continue
		}
		logger.Infof("%s exit status 0", wantedKey)
	}

	logger.Infof("Finished running %s scripts.", os.Args[1])
}
