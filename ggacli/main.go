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
	}

	timeout                      = flag.Duration("timeout", 10*time.Second, "timeout for connections to command monitor. zero or less is infinite.")
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
