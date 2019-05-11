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

package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"

	"github.com/go-ini/ini"
)

func setEnableWSFC(metadata metadataJSON, enabled string) *metadataJSON {
	metadata.Instance.Attributes.EnableWSFC = enabled
	return &metadata
}

func setWSFCAddresses(metadata metadataJSON, wsfcAddresses string) *metadataJSON {
	metadata.Instance.Attributes.WSFCAddresses = wsfcAddresses
	return &metadata
}

func setWSFCAgentPort(metadata metadataJSON, wsfcPort string) *metadataJSON {
	metadata.Instance.Attributes.WSFCAgentPort = wsfcPort
	return &metadata
}

var (
	testAgent    = getWsfcAgentInstance()
	testMetadata = metadataJSON{}
	testListener = &net.TCPListener{}
)

func TestNewWsfcManager(t *testing.T) {
	type args struct {
		newMetadata *metadataJSON
	}
	tests := []struct {
		name string
		args args
		want *wsfcManager
	}{
		{"empty meta config", args{&testMetadata}, &wsfcManager{agentNewState: stopped, agentNewPort: wsfcDefaultAgentPort, agent: testAgent}},
		{"wsfc enabled", args{setEnableWSFC(testMetadata, "true")}, &wsfcManager{agentNewState: running, agentNewPort: wsfcDefaultAgentPort, agent: testAgent}},
		{"wsfc addrs is set", args{setWSFCAddresses(testMetadata, "0.0.0.0")}, &wsfcManager{agentNewState: running, agentNewPort: wsfcDefaultAgentPort, agent: testAgent}},
		{"wsfc port is set", args{setWSFCAgentPort(testMetadata, "1818")}, &wsfcManager{agentNewState: stopped, agentNewPort: "1818", agent: testAgent}},
	}
	config = ini.Empty()
	for _, tt := range tests {
		newMetadata = tt.args.newMetadata
		if got := newWsfcManager(); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("test case %q: newWsfcManager() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestWsfcManagerDiff(t *testing.T) {
	tests := []struct {
		name string
		m    *wsfcManager
		want bool
	}{
		{"state change from stop to running", &wsfcManager{agentNewState: running, agent: &wsfcAgent{listener: nil}}, true},
		{"state change from running to stop", &wsfcManager{agentNewState: stopped, agent: &wsfcAgent{listener: testListener}}, true},
		{"port changed", &wsfcManager{agentNewPort: "1818", agent: &wsfcAgent{port: wsfcDefaultAgentPort}}, true},
		{"state does not change both running", &wsfcManager{agentNewState: running, agent: &wsfcAgent{listener: testListener}}, false},
		{"state does not change both stopped", &wsfcManager{agentNewState: stopped, agent: &wsfcAgent{listener: nil}}, false},
	}
	for _, tt := range tests {
		if got := tt.m.diff(); got != tt.want {
			t.Errorf("test case %q: wsfcManager.diff() = %v, want %v", tt.name, got, tt.want)
		}

	}
}

func TestWsfcManagerDisabled(t *testing.T) {
	m := &wsfcManager{}
	want := false

	if got := m.disabled(); got != want {
		t.Errorf("wsfcManager.disabled() = %v, want %v", got, want)
	}
}

// Mock health agent for unit testing
type mockAgent struct {
	state       agentState
	port        string
	runError    bool
	stopError   bool
	runInvoked  bool
	stopInvoked bool
}

func (a *mockAgent) getState() agentState {
	return a.state
}

func (a *mockAgent) getPort() string {
	return a.port
}

func (a *mockAgent) setPort(newPort string) {
	a.port = newPort
}

func (a *mockAgent) run() error {
	a.runInvoked = true
	if a.runError {
		return errors.New("Run error")
	}

	a.state = running
	return nil
}

func (a *mockAgent) stop() error {
	a.stopInvoked = true
	if a.stopError {
		return errors.New("Stop error")
	}

	a.state = stopped
	return nil
}

func TestWsfcManagerSet(t *testing.T) {
	tests := []struct {
		name        string
		m           *wsfcManager
		wantErr     bool
		runInvoked  bool
		stopInvoked bool
	}{
		{"set start agent", &wsfcManager{agentNewState: running, agent: &mockAgent{state: stopped}}, false, true, false},
		{"set start agent error", &wsfcManager{agentNewState: running, agent: &mockAgent{state: stopped, runError: true}}, true, true, false},
		{"set stop agent", &wsfcManager{agentNewState: stopped, agent: &mockAgent{state: running}}, false, false, true},
		{"set stop agent error", &wsfcManager{agentNewState: stopped, agent: &mockAgent{state: running, stopError: true}}, true, false, true},
		{"set restart agent", &wsfcManager{agentNewState: running, agentNewPort: "1", agent: &mockAgent{state: running, port: "0"}}, false, true, true},
		{"set restart agent stop error", &wsfcManager{agentNewState: running, agentNewPort: "1", agent: &mockAgent{state: running, port: "0", stopError: true}}, true, false, true},
		{"set restart agent start error", &wsfcManager{agentNewState: running, agentNewPort: "1", agent: &mockAgent{state: running, port: "0", runError: true}}, true, true, true},
		{"set do nothing", &wsfcManager{agentNewState: stopped, agentNewPort: "1", agent: &mockAgent{state: stopped, port: "0"}}, false, false, false},
	}
	for _, tt := range tests {
		if err := tt.m.set(); (err != nil) != tt.wantErr {
			t.Errorf("wsfcManager.set() error = %v, wantErr %v", err, tt.wantErr)
		}

		mAgent := tt.m.agent.(*mockAgent)
		if gotRunInvoked := mAgent.runInvoked; gotRunInvoked != tt.runInvoked {
			t.Errorf("wsfcManager.set() runInvoked = %v, want %v", gotRunInvoked, tt.runInvoked)
		}

		if gotStopInvoked := mAgent.stopInvoked; gotStopInvoked != tt.stopInvoked {
			t.Errorf("wsfcManager.set() stopInvoked = %v, want %v", gotStopInvoked, tt.stopInvoked)
		}

		if tt.m.agentNewPort != mAgent.port {
			t.Errorf("wsfcManager.set() does not set prot, agent port = %v, want %v", mAgent.port, tt.m.agentNewPort)
		}
	}
}

func getHealthCheckResponce(request string, agent healthAgent) (string, error) {
	serverAddr := "localhost:" + agent.getPort()
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	fmt.Fprintf(conn, request)
	return bufio.NewReader(conn).ReadString('\n')
}

func TestWsfcRunAgentE2E(t *testing.T) {

	wsfcMgr := &wsfcManager{agentNewState: running, agentNewPort: wsfcDefaultAgentPort, agent: getWsfcAgentInstance()}
	wsfcMgr.set()

	// make sure the agent is cleaned up.
	defer wsfcMgr.agent.stop()

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal("getting localing interface failed.")
	}

	// pick first local ip that is not lookback ip
	var existIP string
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			existIP = ipnet.IP.To4().String()
			break
		}
	}

	// test with existing IP
	if got, err := getHealthCheckResponce(existIP, wsfcMgr.agent); got != "1" {
		t.Errorf("health check failed with %v, got = %v, want %v", existIP, got, "1")
		if err != nil {
			t.Error(err)
		}
	}

	// test an invalid ip which could not exist
	invalidIP := "255.255.255.256"
	if got, err := getHealthCheckResponce(invalidIP, wsfcMgr.agent); got != "0" {
		t.Errorf("health check failed with %v, got = %v, want %v", invalidIP, got, "0")
		if err != nil {
			t.Error(err)
		}
	}

	// test stop agent
	wsfcMgrStop := &wsfcManager{agentNewState: stopped, agent: getWsfcAgentInstance()}
	wsfcMgrStop.set()
	if _, err := getHealthCheckResponce(existIP, wsfcMgr.agent); err == nil {
		t.Errorf("health check still running after calling stop")
	}
}

func TestInvokeRunOnRunningWsfcAgent(t *testing.T) {
	agent := &wsfcAgent{listener: testListener}

	if err := agent.run(); err != nil {
		t.Errorf("Invoke run on running agent, error = %v, want = %v", err, nil)
	}
}

func TestInvokeStopOnStoppedWsfcAgent(t *testing.T) {
	agent := &wsfcAgent{listener: nil}

	if err := agent.stop(); err != nil {
		t.Errorf("Invoke stop on stopped agent, error = %v, want = %v", err, nil)
	}
}

func TestWsfcAgentSetPort(t *testing.T) {
	want := "2"
	agent := &wsfcAgent{port: "1"}
	agent.setPort(want)

	if agent.port != want {
		t.Errorf("WsfcAgent.setPort() port = %v, want %v", agent.port, want)
	}
}

func TestGetWsfcAgentInstance(t *testing.T) {
	agentFirst := getWsfcAgentInstance()
	agentSecond := getWsfcAgentInstance()

	if agentFirst != agentSecond {
		t.Errorf("getWsfcAgentInstance is not returning same instance")
	}
}
