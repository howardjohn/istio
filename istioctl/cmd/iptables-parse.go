// Copyright © 2021 NAME HERE <EMAIL ADDRESS>
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

package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/tools/istio-iptables/pkg/builder"
)

func parseIptablesLine(l string) string {
	i := strings.Index(l, " IPT")
	if i < 0 {
		return ""
	}
	l = l[i+1:]
	endPrefix := strings.Index(l, ":")
	if endPrefix < 0 {
		return ""
	}
	spl := strings.Split(l[:endPrefix], " ")
	if len(spl) != 2 {
		return ""
	}
	command := spl[0]
	comment := builder.AllComments[builder.CommandID(command)]
	source := spl[1]
	data := map[string]string{
		"command": command,
		"comment": comment,
		"source":  source,
	}
	kvPairs := strings.Split(l[endPrefix+1:], " ")
	for _, pair := range kvPairs {
		spl := strings.Split(pair, "=")
		if len(spl) != 2 {
			continue
		}
		if spl[1] == "0" || spl[1] == "0x00" || spl[1] == "" {
			continue
		}
		data[strings.ToLower(spl[0])] = spl[1]
	}
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(b)
}

func parseIptablesLogCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var skipControlPlane bool
	// cmd represents the upgradeCheck command
	cmd := &cobra.Command{
		Use:     "parse-iptables-log",
		Short:   "command to parse iptables logs",
		Long:    `command to parse iptables logs`,
		Example: `tail -f /var/log/kern.log | istioctl x parse-iptables-log`,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			scan := bufio.NewScanner(os.Stdin)
			for scan.Scan() {
				result := parseIptablesLine(scan.Text())
				if result != "" {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), result)
				}
			}
			return nil
		},
	}
	cmd.PersistentFlags().BoolVar(&skipControlPlane, "skip-controlplane", false, "skip checking the control plane")
	opts.AttachControlPlaneFlags(cmd)
	return cmd
}
