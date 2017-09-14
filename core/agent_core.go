// Copyright (c) 2017 Cisco and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package core

import (
	"errors"
	"fmt"
	"time"

	"github.com/ligato/cn-infra/logging"
	"github.com/ligato/cn-infra/utils/safeclose"
	"github.com/namsral/flag"
)

// variables set by the Makefile using ldflags
var (
	BuildVersion string
	BuildDate    string
)

// Agent implements startup & shutdown procedure.
type Agent struct {
	// plugin list
	plugins []*NamedPlugin
	logging.Logger
	// agent startup details
	startup
}

type startup struct {
	// The startup/initialization must take no longer that maxStartup.
	MaxStartupTime time.Duration
	// successfully initialized plugins
	initSuccess []*NamedPlugin
	// init duration in ns
	initDuration int64
	// successfully after-initialized plugins
	afterInitSuccess []*NamedPlugin
	// after-init duration in ns
	afterInitDuration int64
	// the field is set before initialization of every plugin with its name
	currentlyProcessing string
}

const (
	logErrorFmt       = "plugin %s: init error '%s', duration %d"
	logSuccessFmt     = "plugin %s: init success"
	logPostErrorFmt   = "plugin %s: post-init error '%s', duration %d"
	logPostSuccessFmt = "plugin %s: post-init success"
	logTimeoutFmt     = "plugin %s not completed before timeout"
)

// NewAgent returns a new instance of the Agent with plugins.
func NewAgent(logger logging.Logger, maxStartup time.Duration, plugins ...*NamedPlugin) *Agent {
	a := Agent{
		plugins,
		logger,
		startup{MaxStartupTime: maxStartup},
	}
	return &a
}

// Start starts/initializes all plugins on the list.
// First it runs Init() method among all plugins in the list
// Then it tries to run AfterInit() method among all plugins t
// hat implements this optional method.
// It stops when first error occurs by calling Close() method
// for already initialized plugins in reverse order.
// The startup/initialization must take no longer that maxStartup.
// duration otherwise error occurs.
func (agent *Agent) Start() error {
	agent.WithFields(logging.Fields{"BuildVersion": BuildVersion, "BuildDate": BuildDate}).Info("Starting the agent...")

	doneChannel := make(chan struct{}, 0)
	errChannel := make(chan error, 0)

	if !flag.Parsed() {
		flag.Parse()
	}

	go func() {
		err := agent.initPlugins()
		if err != nil {
			errChannel <- err
			return
		}
		err = agent.handleAfterInit()
		if err != nil {
			errChannel <- err
			return
		}
		close(doneChannel)
	}()

	//block until all Plugins are initialized or timeout expires
	select {
	case err := <-errChannel:
		errInit := agent.calculateDiff(agent.initSuccess)
		errAfterInit := agent.calculateDiff(agent.afterInitSuccess)
		agent.WithFields(logging.Fields{"AfterInitFail: ": errAfterInit, "AfterInit succ: ": agent.afterInitSuccess,
			"Init succ: ": agent.initSuccess, "Init fail: ": errInit}).Error("Agent failed to start")

		// Error is logged in handleInit/AfterInit
		return err
	case <-doneChannel:
		agent.WithField("durationNs:", agent.initDuration+agent.afterInitDuration).Info("All plugins initialized successfully")
		return nil
	case <-time.After(agent.MaxStartupTime):
		errInit := agent.calculateDiff(agent.initSuccess)
		errAfterInit := agent.calculateDiff(agent.afterInitSuccess)
		agent.WithFields(logging.Fields{"AfterInitFail: ": errAfterInit, "AfterInit succ: ": agent.afterInitSuccess,
			"Init succ: ": agent.initSuccess, "Init fail: ": errInit}).Error("Agent failed to start")

		return fmt.Errorf(logTimeoutFmt, agent.currentlyProcessing)
	}
}

// Stop gracefully shuts down the Agent. It is called usually when the user
// interrupts the Agent from the EventLoopWithInterrupt().
//
// This implementation tries to call Close() method on every plugin on the list
// in revers order. It continues event if some error occurred.
func (agent *Agent) Stop() error {
	agent.Info("Stopping agent...")
	errMsg := ""
	for i := len(agent.plugins) - 1; i >= 0; i-- {
		agent.WithField("pluginName", agent.plugins[i].PluginName).Debug("Stopping plugin begin")
		err := safeclose.Close(agent.plugins[i].Plugin)
		if err != nil {
			if len(errMsg) > 0 {
				errMsg += "; "
			}
			errMsg += string(agent.plugins[i].PluginName)
			errMsg += ": " + err.Error()
		}
		agent.WithField("pluginName", agent.plugins[i].PluginName).Debug("Stopping plugin end ", err)
	}

	agent.Debug("Agent stopped")

	if len(errMsg) > 0 {
		return errors.New(errMsg)
	}
	return nil
}

// initPlugins calls Init() an all plugins on the list
func (agent *Agent) initPlugins() error {
	startTime := time.Now()
	for i, plug := range agent.plugins {
		// set currently initialized plugin name
		agent.currentlyProcessing = string(plug.PluginName + " Init()")
		err := plug.Init()
		if err != nil {
			//Stop the plugins that are initialized
			for j := i; j >= 0; j-- {
				err := safeclose.Close(agent.plugins[j])
				if err != nil {
					agent.Warn("err closing ", agent.plugins[j].PluginName, " ", err)
				}
			}
			initErrTime := time.Since(startTime)
			return fmt.Errorf(logErrorFmt, plug.PluginName, err, initErrTime.Nanoseconds())
		}

		agent.Info(fmt.Sprintf(logSuccessFmt, plug.PluginName))
		agent.initSuccess = append(agent.initSuccess, plug)
	}
	agent.initDuration = time.Since(startTime).Nanoseconds()

	return nil
}

// handleAfterInit calls the AfterInit handlers for plugins that can only
// finish their initialization after  all other plugins have been initialized.
func (agent *Agent) handleAfterInit() error {
	startTime := time.Now()
	for _, plug := range agent.plugins {
		// set currently after-initialized plugin name
		agent.currentlyProcessing = string(plug.PluginName + " AfterInit()")
		if plug2, ok := plug.Plugin.(PostInit); ok {
			agent.Debug("afterInit begin for ", plug.PluginName)
			err := plug2.AfterInit()
			if err != nil {
				agent.Stop()
				afterInitErrTime := time.Since(startTime)
				return fmt.Errorf(logPostErrorFmt, plug.PluginName, err, afterInitErrTime.Nanoseconds())
			}
			agent.Info(fmt.Sprintf(logPostSuccessFmt, plug.PluginName))
			agent.afterInitSuccess = append(agent.afterInitSuccess, plug)
		}
	}
	agent.afterInitDuration = time.Since(startTime).Nanoseconds()

	return nil
}

// Returns list of plugins which are not initialized
func (agent *Agent) calculateDiff(initialized []*NamedPlugin) []*NamedPlugin {
	var diff []*NamedPlugin
	for _, plugin := range agent.plugins {
		var found bool
		for _, initialized := range initialized {
			if plugin == initialized {
				found = true
			}
		}
		if !found {
			diff = append(diff, plugin)
		}
	}
	return diff
}
