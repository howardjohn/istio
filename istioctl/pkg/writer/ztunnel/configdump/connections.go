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

package configdump

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
)

type ConnectionsFilter struct {
	Namespace string
	Direction string
	Raw       bool
}

func (c *ConfigWriter) PrintConnectionsDump(filter ConnectionsFilter, outputFormat string) error {
	d := c.ztunnelDump
	workloads := maps.Values(d.WorkloadState)
	workloads = slices.SortFunc(workloads, func(a, b WorkloadState) int {
		if r := cmp.Compare(a.Info.Namespace, b.Info.Namespace); r != 0 {
			return r
		}
		return cmp.Compare(a.Info.Namespace, b.Info.Namespace)
	})
	workloads = slices.FilterInPlace(workloads, func(state WorkloadState) bool {
		if filter.Namespace != "" && filter.Namespace != state.Info.Namespace {
			return false
		}
		return true
	})
	out, err := json.MarshalIndent(workloads, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal workloads: %v", err)
	}
	if outputFormat == "yaml" {
		if out, err = yaml.JSONToYAML(out); err != nil {
			return err
		}
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}

func (c *ConfigWriter) PrintConnectionsSummary(filter ConnectionsFilter) error {
	w := c.tabwriter()
	d := c.ztunnelDump
	serviceNames := map[string]string{}
	workloadNames := map[string]string{}
	for netIP, s := range d.Services {
		_, ip, _ := strings.Cut(netIP, "/")
		serviceNames[ip] = s.Hostname
	}
	for netIP, s := range d.Workloads {
		_, ip, _ := strings.Cut(netIP, "/")
		workloadNames[ip] = s.Name + "." + s.Namespace
	}
	lookupIp := func(addr string) string {
		if filter.Raw {
			return addr
		}
		ip, port, _ := net.SplitHostPort(addr)
		if s, f := serviceNames[ip]; f {
			return net.JoinHostPort(s, port)
		}
		if w, f := workloadNames[ip]; f {
			return net.JoinHostPort(w, port)
		}
		return addr
	}
	fmt.Fprintln(w, "WORKLOAD\tDIRECTION\tLOCAL\tREMOTE\tREMOTE TARGET")
	workloads := maps.Values(d.WorkloadState)
	workloads = slices.SortFunc(workloads, func(a, b WorkloadState) int {
		if r := cmp.Compare(a.Info.Namespace, b.Info.Namespace); r != 0 {
			return r
		}
		return cmp.Compare(a.Info.Namespace, b.Info.Namespace)
	})
	for _, wl := range workloads {
		if filter.Namespace != "" && filter.Namespace != wl.Info.Namespace {
			continue
		}
		name := fmt.Sprintf("%s.%s", wl.Info.Name, wl.Info.Namespace)
		if filter.Direction != "outbound" {
			for _, c := range wl.Connections.Inbound {
				fmt.Fprintf(w, "%v\tInbound\t%v\t%v\t\n", name, lookupIp(c.Dst), lookupIp(c.Src))
			}
		}
		if filter.Direction != "inbound" {
			for _, c := range wl.Connections.Outbound {
				fmt.Fprintf(w, "%v\tOutbound\t%v\t%v\t%v\n", name, lookupIp(c.Src), lookupIp(c.ActualDst), lookupIp(c.OriginalDst))
			}
		}
	}
	return w.Flush()
}
