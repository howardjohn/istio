// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package monitoring

import (
	"fmt"
	"net"
	"net/http"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/network"
)

func SetupMonitoring(port int, path string, stop <-chan struct{}) error {
	if port <= 0 {
		return nil
	}
	mux := http.NewServeMux()
	var listener net.Listener
	var err error
	if listener, err = net.Listen("tcp", fmt.Sprintf(":%d", port)); err != nil {
		log.Errorf("unable to listen on socket: %v", err)
		return err
	}
	exporter, err := monitoring.RegisterPrometheusExporter(nil, nil)
	if err != nil {
		log.Errorf("could not set up prometheus exporter: %v", err)
		return err
	}
	mux.Handle(path, exporter)
	monitoringServer := &http.Server{
		Handler: mux,
	}
	go func() {
		if err := monitoringServer.Serve(listener); network.IsUnexpectedListenerError(err) {
			log.Errorf("error running monitoring http server: %s", err)
		}
	}()
	go func() {
		<-stop
		err := monitoringServer.Close()
		log.Debugf("monitoring server terminated: %v", err)
	}()

	return nil
}
