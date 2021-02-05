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

package cmd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	prometheus "github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

func toHostsList(src string, ns []string, svc []string, domainSuffix string) []string {
	res := []string{"./*"}
	for _, s := range ns {
		if s == src || s == "unknown" {
			continue
		}
		res = append(res, s+"/*")
	}
	for _, s := range svc {
		if strings.HasSuffix(s, domainSuffix) {
			continue
		}
		res = append(res, "*/"+s)
	}
	sort.Strings(res)
	return res
}

func generateSidecarCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var promURI, domainSuffix string
	cmd := &cobra.Command{
		Use:   "generate-sidecar",
		Short: "automatically create a Sidecar scoping for each namespace",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			nsDeps, err := getNamespaceDependencies(client, promURI)
			if err != nil {
				return fmt.Errorf("failed to query protocols: %v", err)
			}

			seDeps, err := getServiceEntryDependencies(client, promURI)
			if err != nil {
				return fmt.Errorf("failed to query protocols: %v", err)
			}
			results := []string{}
			for src, dsts := range nsDeps {
				extraDsts := seDeps[src]
				sidecar := config.Config{
					Meta: config.Meta{
						Namespace:        src,
						Name:             "default",
						GroupVersionKind: gvk.Sidecar,
					},
					Spec: &networking.Sidecar{
						Egress: []*networking.IstioEgressListener{{
							Hosts: toHostsList(src, dsts, extraDsts, domainSuffix),
						}},
					},
				}
				cfg, err := crd.ConvertConfig(sidecar)
				if err != nil {
					return err
				}
				y, err := yaml.Marshal(cfg)
				if err != nil {
					return err
				}
				results = append(results, string(y))
			}
			fmt.Println(strings.Join(results, "\n---\n"))
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&promURI, "prometheus-uri", "", "The URI of the prometheus instance "+
		"which is recording telemetry for the mesh.  Overrides port forwarding behavior")
	cmd.PersistentFlags().StringVar(&domainSuffix, "domain-suffix", constants.DefaultKubernetesDomain, "The domain suffix used for mesh services")
	cmd.PersistentFlags().StringVar(&addonNamespace, "namespace", istioNamespace,
		"Namespace where the addon is running, if not specified, istio-system would be used")
	return cmd
}

func getNamespaceDependencies(client kube.ExtendedClient, promURI string) (map[string][]string, error) {
	var addr string
	if len(promURI) < 1 {
		pl, err := client.PodsForSelector(context.TODO(), addonNamespace, "app=prometheus")
		if err != nil {
			return nil, fmt.Errorf("not able to locate Prometheus pod: %v", err)
		}

		if len(pl.Items) < 1 {
			return nil, errors.New("no Prometheus pods found")
		}

		// only use the first pod in the list
		addr, err = portForwardAsync(pl.Items[0].Name, addonNamespace, "Prometheus", bindAddress, 9090, client)
		if err != nil {
			return nil, fmt.Errorf("not able to forward to Prometheus: %v", err)
		}
	} else {
		addr = promURI
	}

	pclient, err := prometheus.NewClient(prometheus.Config{
		Address: fmt.Sprintf("http://%s", addr),
	})
	if err != nil {
		return nil, fmt.Errorf("could not connect to Prometheus at %s: %v", addr, err)
	}

	prom := v1.NewAPI(pclient)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	result, warnings, err := prom.Query(ctx, `sum(istio_requests_total{source_app!="istio-ingressgateway"}) by (source_workload_namespace, destination_workload_namespace)`, time.Now())
	if err != nil {
		return nil, fmt.Errorf("error querying Prometheus: %v", err)
	}
	if len(warnings) > 0 {
		fmt.Printf("Warnings: %v\n", warnings)
	}
	rv := result.(model.Vector)
	srcToDest := make(map[string][]string)
	for _, i := range rv {
		src, ok := i.Metric["source_workload_namespace"]
		if !ok {
			fmt.Printf("WARNING: no source_workload_namespace in %v/n", i)
		}
		dst, ok := i.Metric["destination_workload_namespace"]
		if !ok {
			fmt.Printf("WARNING: no destination_workload_namespace in %v/n", i)
		}
		srcToDest[string(src)] = append(srcToDest[string(src)], string(dst))
	}
	return srcToDest, nil
}

func getServiceEntryDependencies(client kube.ExtendedClient, promURI string) (map[string][]string, error) {
	var addr string
	if len(promURI) < 1 {
		pl, err := client.PodsForSelector(context.TODO(), addonNamespace, "app=prometheus")
		if err != nil {
			return nil, fmt.Errorf("not able to locate Prometheus pod: %v", err)
		}

		if len(pl.Items) < 1 {
			return nil, errors.New("no Prometheus pods found")
		}

		// only use the first pod in the list
		addr, err = portForwardAsync(pl.Items[0].Name, addonNamespace, "Prometheus", bindAddress, 9090, client)
		if err != nil {
			return nil, fmt.Errorf("not able to forward to Prometheus: %v", err)
		}
	} else {
		addr = promURI
	}

	pclient, err := prometheus.NewClient(prometheus.Config{
		Address: fmt.Sprintf("http://%s", addr),
	})
	if err != nil {
		return nil, fmt.Errorf("could not connect to Prometheus at %s: %v", addr, err)
	}

	prom := v1.NewAPI(pclient)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	result, warnings, err := prom.Query(ctx, `sum(istio_requests_total{destination_workload_namespace="unknown", destination_service!="unknown"}) by (source_workload_namespace, destination_service)`, time.Now())
	if err != nil {
		return nil, fmt.Errorf("error querying Prometheus: %v", err)
	}
	if len(warnings) > 0 {
		fmt.Printf("Warnings: %v\n", warnings)
	}
	rv := result.(model.Vector)
	srcToDest := make(map[string][]string)
	for _, i := range rv {
		src, ok := i.Metric["source_workload_namespace"]
		if !ok {
			fmt.Printf("WARNING: no source_workload_namespace in %v/n", i)
		}
		dst, ok := i.Metric["destination_service"]
		if !ok {
			fmt.Printf("WARNING: no destination_service in %v/n", i)
		}
		srcToDest[string(src)] = append(srcToDest[string(src)], string(dst))
	}
	return srcToDest, nil
}

// portForwardAsync first tries to forward localhost:remotePort to podName:remotePort, falls back to dynamic local port
func portForwardAsync(podName, namespace, flavor, localAddress string, remotePort int, client kube.ExtendedClient) (string, error) {
	// port preference:
	// - If --listenPort is specified, use it
	// - without --listenPort, prefer the remotePort but fall back to a random port
	var portPrefs []int
	if listenPort != 0 {
		portPrefs = []int{listenPort}
	} else {
		portPrefs = []int{remotePort, 0}
	}

	var err error
	for _, localPort := range portPrefs {
		fw, err := client.NewPortForwarder(podName, namespace, localAddress, localPort, remotePort)
		if err != nil {
			return "", fmt.Errorf("could not build port forwarder for %s: %v", flavor, err)
		}

		if err = fw.Start(); err != nil {
			// Try the next port
			continue
		}

		// Close the port forwarder when the command is terminated.
		closePortForwarderOnInterrupt(fw)
		log.Debugf(fmt.Sprintf("port-forward to %s pod ready", flavor))
		return fw.Address(), nil
	}
	return "", fmt.Errorf("failure running port forward: %v", err)
}
