// Copyright 2017 Istio Authors
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

package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/pkg/collateral"
	"istio.io/pkg/log"
	"istio.io/pkg/probe"
	"istio.io/pkg/version"
)

var (
	flags = struct {
		loggingOptions *log.Options

		meshconfig             string
		injectConfigFile       string
		injectValuesFile       string
		certFile               string
		privateKeyFile         string
		caCertFile             string
		port                   int
		healthCheckInterval    time.Duration
		healthCheckFile        string
		probeOptions           probe.Options
		kubeconfigFile         string
		webhookConfigName      string
		webhookName            string
		monitoringPort         int
		reconcileWebhookConfig bool
	}{
		loggingOptions: log.DefaultOptions(),
	}

	rootCmd = &cobra.Command{
		Use:          "sidecar-injector",
		Short:        "Kubernetes webhook for automatic Istio sidecar injection.",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(0),
		RunE: func(c *cobra.Command, _ []string) error {
			cmd.PrintFlags(c.Flags())
			if err := log.Configure(flags.loggingOptions); err != nil {
				return err
			}

			log.Infof("version %s", version.Info.String())

			parameters := inject.WebhookParameters{
				ConfigFile:          flags.injectConfigFile,
				ValuesFile:          flags.injectValuesFile,
				MeshFile:            flags.meshconfig,
				CertFile:            flags.certFile,
				KeyFile:             flags.privateKeyFile,
				Port:                flags.port,
				HealthCheckInterval: flags.healthCheckInterval,
				HealthCheckFile:     flags.healthCheckFile,
				MonitoringPort:      flags.monitoringPort,
			}
			wh, err := inject.NewWebhook(parameters)
			if err != nil {
				return multierror.Prefix(err, "failed to create injection webhook")
			}

			stop := make(chan struct{})
			if flags.reconcileWebhookConfig {
				params := inject.WebhookCertParams{
					CaCertFile:        flags.caCertFile,
					KubeconfigFile:    flags.kubeconfigFile,
					WebhookConfigName: flags.webhookConfigName,
					WebhookName:       flags.webhookName,
				}
				if err := inject.PatchCertLoop(stop, params); err != nil {
					return multierror.Prefix(err, "failed to start patch cert loop")
				}
			}

			go wh.Run(stop)
			cmd.WaitSignal(stop)
			return nil
		},
	}

	probeCmd = &cobra.Command{
		Use:   "probe",
		Short: "Check the liveness or readiness of a locally-running server",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !flags.probeOptions.IsValid() {
				return errors.New("some options are not valid")
			}
			if err := probe.NewFileClient(&flags.probeOptions).GetStatus(); err != nil {
				return fmt.Errorf("fail on inspecting path %s: %v", flags.probeOptions.Path, err)
			}
			fmt.Println("OK")
			return nil
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVar(&flags.meshconfig, "meshConfig", "/etc/istio/config/mesh",
		"File containing the Istio mesh configuration")
	rootCmd.PersistentFlags().StringVar(&flags.injectConfigFile, "injectConfig", "/etc/istio/inject/config",
		"File containing the Istio sidecar injection configuration and template")
	rootCmd.PersistentFlags().StringVar(&flags.injectValuesFile, "injectValues", "/etc/istio/inject/values",
		"File containing the Istio sidecar injection values, in yaml format")
	rootCmd.PersistentFlags().StringVar(&flags.certFile, "tlsCertFile", "/etc/istio/certs/cert-chain.pem",
		"File containing the x509 Certificate for HTTPS.")
	rootCmd.PersistentFlags().StringVar(&flags.privateKeyFile, "tlsKeyFile", "/etc/istio/certs/key.pem",
		"File containing the x509 private key matching --tlsCertFile.")
	rootCmd.PersistentFlags().StringVar(&flags.caCertFile, "caCertFile", "/etc/istio/certs/root-cert.pem",
		"File containing the x509 Certificate for HTTPS.")
	rootCmd.PersistentFlags().IntVar(&flags.port, "port", 9443, "Webhook port")
	rootCmd.PersistentFlags().IntVar(&flags.monitoringPort, "monitoringPort", 15014, "Webhook monitoring port")

	rootCmd.PersistentFlags().DurationVar(&flags.healthCheckInterval, "healthCheckInterval", 0,
		"Configure how frequently the health check file specified by --healthCheckFile should be updated")
	rootCmd.PersistentFlags().StringVar(&flags.healthCheckFile, "healthCheckFile", "",
		"File that should be periodically updated if health checking is enabled")
	rootCmd.PersistentFlags().StringVar(&flags.kubeconfigFile, "kubeconfig", "",
		"Specifies path to kubeconfig file. This must be specified when not running inside a Kubernetes pod.")
	rootCmd.PersistentFlags().StringVar(&flags.webhookConfigName, "webhookConfigName", "istio-sidecar-injector",
		"Name of the mutatingwebhookconfiguration resource in Kubernetes.")
	rootCmd.PersistentFlags().StringVar(&flags.webhookName, "webhookName", "sidecar-injector.istio.io",
		"Name of the webhook entry in the webhook config.")
	rootCmd.PersistentFlags().BoolVar(&flags.reconcileWebhookConfig, "reconcileWebhookConfig", true,
		"Enable managing webhook configuration.")
	// Attach the Istio logging options to the command.
	flags.loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Sidecar Injector",
		Section: "sidecar-injector CLI",
		Manual:  "Istio Sidecar Injector",
	}))

	probeCmd.PersistentFlags().StringVar(&flags.probeOptions.Path, "probe-path", "",
		"Path of the file for checking the availability.")
	probeCmd.PersistentFlags().DurationVar(&flags.probeOptions.UpdateInterval, "interval", 0,
		"Duration used for checking the target file's last modified time.")
	rootCmd.AddCommand(probeCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
