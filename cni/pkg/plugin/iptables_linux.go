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

// This is a sample chained plugin that supports multiple CNI versions. It
// parses prevResult according to the cniVersion
package plugin

import (
	"fmt"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/spf13/viper"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/cmd"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	"istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

// getNs is a unit test override variable for interface create.
var getNs = ns.GetNS

// Program defines a method which programs iptables based on the parameters
// provided in Redirect.
func (ipt *iptables) Program(podName, netns string, rdrct *Redirect) error {
	// TODO: global is bad
	log.Errorf("howardjohn: %#v", rdrct)
	setDefaults()
	viper.Set(constants.CNIMode, true)
	viper.Set(constants.NetworkNamespace, netns)
	viper.Set(constants.EnvoyPort, rdrct.targetPort)
	viper.Set(constants.ProxyUID, rdrct.noRedirectUID)
	viper.Set(constants.ProxyGID, rdrct.noRedirectGID)
	viper.Set(constants.InboundInterceptionMode, rdrct.redirectMode)
	viper.Set(constants.ServiceCidr, rdrct.includeIPCidrs)
	viper.Set(constants.LocalExcludePorts, rdrct.excludeInboundPorts)
	viper.Set(constants.InboundPorts, rdrct.includeInboundPorts)
	viper.Set(constants.ExcludeInterfaces, rdrct.excludeInterfaces)
	viper.Set(constants.LocalOutboundPortsExclude, rdrct.excludeOutboundPorts)
	viper.Set(constants.OutboundPorts, rdrct.includeOutboundPorts)
	viper.Set(constants.ServiceExcludeCidr, rdrct.excludeIPCidrs)
	viper.Set(constants.KubeVirtInterfaces, rdrct.kubevirtInterfaces)
	viper.Set(constants.DryRun, dependencies.DryRunFilePath.Get() != "")
	viper.Set(constants.RedirectDNS, rdrct.dnsRedirect)
	viper.Set(constants.CaptureAllDNS, rdrct.dnsRedirect)
	viper.Set(constants.DropInvalid, rdrct.invalidDrop)
	viper.Set(constants.DualStack, rdrct.dualStack)
	cfg := cmd.ConstructConfig() // HACK. TODO: Stop abusing globals

	netNs, err := getNs(netns)
	if err != nil {
		err = fmt.Errorf("failed to open netns %q: %s", netns, err)
		return err
	}
	defer netNs.Close()

	if err = netNs.Do(func(_ ns.NetNS) error {
		log.Infof("============= Start iptables configuration for %v =============", podName)
		defer log.Infof("============= End iptables configuration for %v =============", podName)
		cmd.RunIptables(cfg)
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func setDefaults() {
	bind := func(name string, def any) {
		viper.SetDefault(name, def)
	}

	bind(constants.EnvoyPort, "15001")
	bind(constants.InboundCapturePort, "15006")
	bind(constants.InboundTunnelPort, "15008")
	bind(constants.ProxyUID, "")
	bind(constants.ProxyGID, "")
	bind(constants.InboundInterceptionMode, "")
	bind(constants.InboundPorts, "")
	bind(constants.LocalExcludePorts, "")
	bind(constants.ExcludeInterfaces, "")
	bind(constants.ServiceCidr, "")
	bind(constants.ServiceExcludeCidr, "")
	bind(constants.OwnerGroupsInclude.Name, constants.OwnerGroupsInclude.DefaultValue)
	bind(constants.OwnerGroupsExclude.Name, constants.OwnerGroupsExclude.DefaultValue)
	bind(constants.OutboundPorts, "")
	bind(constants.LocalOutboundPortsExclude, "")
	bind(constants.KubeVirtInterfaces, "")
	bind(constants.InboundTProxyMark, "1337")
	bind(constants.InboundTProxyRouteTable, "133")
	bind(constants.DryRun, false)
	bind(constants.TraceLogging, false)
	bind(constants.RestoreFormat, true)
	bind(constants.IptablesProbePort, constants.DefaultIptablesProbePort)
	bind(constants.ProbeTimeout, constants.DefaultProbeTimeout)
	bind(constants.SkipRuleApply, false)
	bind(constants.RunValidation, false)
	bind(constants.CaptureAllDNS, false)
	bind(constants.NetworkNamespace, "")
	bind(constants.CNIMode, false)
	bind(constants.IptablesVersion, "")
}
