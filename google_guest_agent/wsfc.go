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
	"net"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const wsfcDefaultAgentPort = "59998"

type agentState int

// Enum for agentState
const (
	running agentState = iota
	stopped
)

var (
	once          sync.Once
	agentInstance *wsfcAgent
)

type wsfcManager struct {
	agentNewState agentState
	agentNewPort  string
	agent         healthAgent
}

// Create new wsfcManager based on metadata agent request state will be set to
// running if one of the following is true:
// - EnableWSFC is set
// - WSFCAddresses is set (As an advanced setting, it will always override EnableWSFC flag)
func newWsfcManager() *wsfcManager {
	newState := stopped

	if func() bool {
		enabled, err := config.Section("wsfc").Key("enable").Bool()
		if err == nil {
			return enabled
		}
		if config.Section("wsfc").Key("addresses").String() != "" {
			return true
		}
		if newMetadata.Instance.Attributes.EnableWSFC != nil {
			return *newMetadata.Instance.Attributes.EnableWSFC
		}
		if newMetadata.Instance.Attributes.WSFCAddresses != "" {
			return true
		}
		if newMetadata.Project.Attributes.EnableWSFC != nil {
			return *newMetadata.Project.Attributes.EnableWSFC
		}
		if newMetadata.Project.Attributes.WSFCAddresses != "" {
			return true
		}
		return false
	}() {
		newState = running
	}

	newPort := wsfcDefaultAgentPort
	port := config.Section("wsfc").Key("port").String()
	if port != "" {
		newPort = port
	} else if newMetadata.Instance.Attributes.WSFCAgentPort != "" {
		newPort = newMetadata.Instance.Attributes.WSFCAgentPort
	} else if newMetadata.Project.Attributes.WSFCAgentPort != "" {
		newPort = newMetadata.Instance.Attributes.WSFCAgentPort
	}

	return &wsfcManager{agentNewState: newState, agentNewPort: newPort, agent: getWsfcAgentInstance()}
}

// Implement manager.diff()
func (m *wsfcManager) diff() bool {
	return m.agentNewState != m.agent.getState() || m.agentNewPort != m.agent.getPort()
}

// Implement manager.disabled().
// wsfc manager is always enabled. The manager is just a broker which manages the state of wsfcAgent. User
// can disable the wsfc feature by setting the metadata. If the manager is disabled, the agent will stop.
func (m *wsfcManager) disabled(os string) bool {
	return false
}

func (m *wsfcManager) timeout() bool {
	return false
}

// Diff will always be called before set. So in set, only two cases are possible:
// - state changed: start or stop the wsfc agent accordingly
// - port changed: restart the agent if it is running
func (m *wsfcManager) set() error {
	m.agent.setPort(m.agentNewPort)

	// if state changes
	if m.agentNewState != m.agent.getState() {
		if m.agentNewState == running {
			return m.agent.run()
		}

		return m.agent.stop()
	}

	// If port changed
	if m.agent.getState() == running {
		if err := m.agent.stop(); err != nil {
			return err
		}

		return m.agent.run()
	}

	return nil
}

// interface for agent answering health check ping
type healthAgent interface {
	getState() agentState
	getPort() string
	setPort(string)
	run() error
	stop() error
}

// Windows failover cluster agent, implements healthAgent interface
type wsfcAgent struct {
	port      string
	waitGroup *sync.WaitGroup
	listener  *net.TCPListener
}

// Start agent and taking tcp request
func (a *wsfcAgent) run() error {
	if a.getState() == running {
		logger.Infof("wsfc agent is already running")
		return nil
	}

	logger.Infof("Starting wsfc agent...")
	listenerAddr, err := net.ResolveTCPAddr("tcp", ":"+a.port)
	if err != nil {
		return err
	}

	listener, err := net.ListenTCP("tcp", listenerAddr)
	if err != nil {
		return err
	}

	// goroutine for handling request
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				// if err is not due to listener closed, return
				if opErr, ok := err.(*net.OpError); ok && strings.Contains(opErr.Error(), "closed") {
					logger.Infof("wsfc agent - tcp listener closed.")
					return
				}

				logger.Errorf("wsfc agent - error on accepting request: %s", err)
				continue
			}
			a.waitGroup.Add(1)
			go a.handleHealthCheckRequest(conn)
		}
	}()

	logger.Infof("wsfc agent started. Listening on port: %s", a.port)
	a.listener = listener

	return nil
}

// Handle health check request.
// The request payload is WSFC ip address.
// Sendback 1 if ipaddress is found locally and 0 otherwise.
func (a *wsfcAgent) handleHealthCheckRequest(conn net.Conn) {
	defer closer(conn)
	defer a.waitGroup.Done()
	conn.SetDeadline(time.Now().Add(time.Second))

	buf := make([]byte, 1024)
	// Read the incoming connection into the buffer.
	reqLen, err := conn.Read(buf)
	if err != nil {
		logger.Errorf("wsfc - error on processing tcp request for network heartbeat health check: %s", err)
		return
	}

	wsfcIP := strings.TrimSpace(string(buf[:reqLen]))
	reply, err := checkIPExist(wsfcIP)
	if err != nil {
		logger.Errorf("wsfc - error on checking local ip: %s", err)
	}
	conn.Write([]byte(reply))
}

// Stop agent. Will wait for all existing request to be completed.
func (a *wsfcAgent) stop() error {
	if a.getState() == stopped {
		logger.Infof("wsfc agent already stopped.")
		return nil
	}

	logger.Infof("Stopping wsfc agent...")
	// close listener first to avoid taking additional request
	err := a.listener.Close()
	// wait for exiting request to finish
	a.waitGroup.Wait()
	a.listener = nil
	logger.Infof("wsfc agent stopped.")
	return err
}

// Get the current state of the agent. If there is a valid listener,
// return state running and if listener is nil, return stopped
func (a *wsfcAgent) getState() agentState {
	if a.listener != nil {
		return running
	}

	return stopped
}

func (a *wsfcAgent) getPort() string {
	return a.port
}

func (a *wsfcAgent) setPort(newPort string) {
	if newPort != a.port {
		logger.Infof("update wsfc agent from port %v to %v", a.port, newPort)
		a.port = newPort
	}
}

// Create wsfc agent only once
func getWsfcAgentInstance() *wsfcAgent {
	once.Do(func() {
		agentInstance = &wsfcAgent{
			port:      wsfcDefaultAgentPort,
			waitGroup: &sync.WaitGroup{},
			listener:  nil,
		}
	})

	return agentInstance
}

// help func to check whether the ip exists on local host.
func checkIPExist(ip string) (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "0", err
	}

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			ipString := ipnet.IP.To4().String()
			if ip == ipString {
				return "1", nil
			}
		}
	}

	return "0", nil
}
