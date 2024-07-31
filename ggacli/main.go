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

// ggacli is a cli tool for interacting with the guest agent via command monitor.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/command"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/hostname"
)

// Action is an action to be invoked by the user.
type Action struct {
	helpmsg string
	fn      ActionFunc
}

// ActionSet is a map of actions to the string needed to run them with ggacli.
type ActionSet map[string]Action

// String returns a usage string for the Actions in the ActionSet.
func (as ActionSet) String() string {
	var s strings.Builder
	for n, a := range as {
		s.WriteString(fmt.Sprintf("  %s\n\t%s\n", n, a.helpmsg))
	}
	return s.String()
}

// Find the named action or return a default error action.
func (as ActionSet) Find(name string) ActionFunc {
	if action, ok := as[name]; ok {
		return action.fn
	}
	return func(context.Context) (string, int) {
		return fmt.Sprintf("Action %s not found.\nactions:\n%s", name, as.String()), 1
	}
}

// ActionFunc is a function to execute the action. It returns an optional
// message and the exit code for ggacli.
type ActionFunc func(context.Context) (string, int)

var (
	defaultActions = ActionSet{
		"sendcmd": {
			helpmsg: "send arbitrary data from the 'json' flag over the command monitor socket",
			fn:      sendcmd,
		},
		"checksocket": {
			helpmsg: "check that the configured command monitor socket is being listened on",
			fn:      checksocket,
		},
		"sethostname": {
			helpmsg: "set the hostname and fqdn to the value found the the MDS",
			fn:      sethostname,
		},
		"getoption": {
			helpmsg: "get the value of a config option from the running guest agent. -section and -key flags specify what option",
			fn: getoption,
		}
	}

	timeout                      = flag.Duration("timeout", 10*time.Second, "timeout for connections to command monitor. zero or less is infinite.")
	jsonPayload                  = flag.String("json", "", "json payload for sendcmd")
	configSection = flag.String("section", "", "config section for getoption")
	configKey = flag.String("key", "", "config key for getoption")
	jsonPayload                  = flag.String("json", "", "json payload for sendcmd")
	help                         = flag.Bool("help", false, "print usage information")
	noopWhenCmdMonitorIsDisabled = flag.Bool("noop_when_cmdmonitor_is_disabled", true, "do nothing if the command monitor is disabled in the instance configuration")
)

func sendcmd(ctx context.Context) (string, int) {
	if *jsonPayload == "" {
		return "sendcmd called with no command", 1
	}
	r := command.SendCommand(ctx, []byte(*jsonPayload))
	var resp command.Response
	if err := json.Unmarshal(r, &resp); err != nil {
		return string(r), 1
	}
	return string(r), resp.Status
}

func sethostname(ctx context.Context) (string, int) {
	msg := []byte(fmt.Sprintf(`{"Command":"%s"}`, hostname.ReconfigureHostnameCommand))
	r := command.SendCommand(ctx, msg)
	var resp hostname.ReconfigureHostnameResponse
	if err := json.Unmarshal(r, &resp); err != nil {
		return string(r), 1
	}
	var out strings.Builder
	out.WriteString(fmt.Sprintf("hostname was set to %s\n", resp.Hostname))
	out.WriteString(fmt.Sprintf("fqdn was set to %s\n", resp.Fqdn))
	if resp.Status != 0 {
		out.WriteString(fmt.Sprintf("%s\n", resp.StatusMessage))
	}
	return out.String(), resp.Status
}

func getoption(ctx context.Context) (string, int) {
	section := *configSection
	key := *configKey
	key, section = transformConfigNames(section,key)
	msg := []byte(fmt.Sprintf(`{"Command":"agent.config.getoption","Option":"%s.%s"}`, section, key))
	r := command.SendCommand(ctx, msg)
	var resp hostname.ReconfigureHostnameResponse
	if err := json.Unmarshal(r, &resp); err != nil {
		return fmt.Sprintf("could not unmarshal response %s: %v", r, err), 1
	}
	return resp.StatusMessage, resp.Status
}

// Turn the ini representation (section: IpForwarding, key: ip_aliases) into
// the struct representation (section: IPForwarding, key: IPAliases)
func transformConfigNames(section, key) (string, string) {
	// Split section name into words
	var sectionwords, keywords []string
	sectionwords = regexp.MustCompile(`[A-Z]+[a-z]*`).FindAllString(section, -1)
	keywords = strings.Split(key, "_")
	fixInitialisms := func(s string) string {
		s = regexp.MustCompile("^[iI]p$").ReplaceAllString(s, "IP")
		s = regexp.MustCompile("^[iI]ps$").ReplaceAllString(s, "IPs")
		s = regexp.MustCompile("^[iI]d$").ReplaceAllString(s, "ID")
		s = strings.ReplaceAll(s, "dhcp", "DHCP")
		s = strings.ReplaceAll(s, "mds", "MDS")
		s = strings.ReplaceAll(s, "mtls", "MTLS")
		s = strings.ReplaceAll(s, "fqdn", "FQDN")
		return s
	)}
	for i := range sectionwords {
		s = fixInitialisms(sectionwords[i])
		s = strings.ToUpper(s[0]) + s[1:]
		sectionwords[i] = s
	}
	for i := range keywords {
		s = fixInitialisms(keywords[i])
		s = strings.ToUpper(s[0]) + s[1:]
		keywords[i] = s
	}
}

func checksocket(ctx context.Context) (string, int) {
	pipe := cfg.Get().Unstable.CommandPipePath
	if pipe == "" {
		pipe = command.DefaultPipePath
	}
	c, err := dial(ctx, pipe)
	if err != nil {
		return fmt.Sprintf("socket does not exist: %v", err), 1
	}
	c.Close()
	return "", 0
}

func main() {
	ctx := context.Background()
	flag.Usage = func() {
		fmt.Printf("%s usage:\n", flag.Arg(0))
		fmt.Printf("actions:\n")
		fmt.Printf(defaultActions.String())
		fmt.Printf("flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	cfg.Load(nil)
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	if *noopWhenCmdMonitorIsDisabled && !cfg.Get().Unstable.CommandMonitorEnabled {
		fmt.Println("command monitor is disabled")
		os.Exit(1)
	}

	actionFn := defaultActions.Find(flag.Arg(0))
	msg, i := actionFn(ctx)
	if msg != "" {
		fmt.Print(msg)
		if !strings.HasSuffix(msg, "\n") {
			fmt.Print("\n")
		}
	}
	os.Exit(i)
}
