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

package httpmux

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/ligato/cn-infra/core"
	"github.com/ligato/cn-infra/logging"
	"github.com/ligato/cn-infra/utils/safeclose"
	"github.com/unrolled/render"
)

const (
	// PluginID used in the Agent Core flavors
	PluginID core.PluginName = "HTTP"
)

// Plugin implements the Plugin interface.
type Plugin struct {
	LogFactory logging.LogFactory
	HTTPport   *HTTPPort
	port 		string

	logging.Logger
	server    *http.Server
	mx        *mux.Router
	formatter *render.Render
}

// Init is entry point called by Agent Core
// - It prepares Gorilla MUX HTTP Router
// - registers grpc transport
func (plugin *Plugin) Init() (err error) {
	plugin.port = plugin.HTTPport.Port
	plugin.Logger, err = plugin.LogFactory.NewLogger(string(PluginID) + "-" + plugin.port)
	if err != nil {
		return err
	}

	plugin.mx = mux.NewRouter()
	plugin.formatter = render.New(render.Options{
		IndentJSON: true,
	})

	// todo grpc temporary disabled
	//plugin.grpcServer = grpcsync.NewAdapter()
	//plugin.Debug("grpctransp: ", plugin.grpcServer)
	//err = datasync.RegisterTransport(&syncbase.Adapter{Watcher: plugin.grpcServer})

	return err
}

// RegisterHTTPHandler propagates to Gorilla mux
func (plugin *Plugin) RegisterHTTPHandler(path string,
	handler func(formatter *render.Render) http.HandlerFunc,
	methods ...string) *mux.Route {
	return plugin.mx.HandleFunc(path, handler(plugin.formatter)).Methods(methods...)
}

// AfterInit starts the HTTP server
func (plugin *Plugin) AfterInit() error {
	address := fmt.Sprintf("0.0.0.0:%s", plugin.port)
	//TODO NICE-to-HAVE make this configurable
	plugin.server = &http.Server{Addr: address, Handler: plugin.mx}

	var errCh chan error
	go func() {
		plugin.Info("Listening on http://", address)

		if err := plugin.server.ListenAndServe(); err != nil {
			errCh <- err
		} else {
			errCh <- nil
		}
	}()

	select {
	case err := <-errCh:
		return err
		// Wait 100ms to create a new stream, so it doesn't bring too much
		// overhead when retry.
	case <-time.After(100 * time.Millisecond):
		//everything is probably fine
		return nil
	}
}

// Close cleans up the resources
func (plugin *Plugin) Close() error {
	err := safeclose.Close(plugin.server)
	return err
}

// HTTPPort contains port value as string
type HTTPPort struct {
	Port string
}