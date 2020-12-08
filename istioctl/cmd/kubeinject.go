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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	admission "k8s.io/api/admission/v1beta1"
	admissionregistration "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/util/podutils"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
)

const (
	configMapKey       = "mesh"
	injectConfigMapKey = "config"
	valuesConfigMapKey = "values"
)

func createInterface(kubeconfig string) (kubernetes.Interface, error) {
	restConfig, err := kube.BuildClientConfig(kubeconfig, configContext)

	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restConfig)
}

func getMeshConfigFromConfigMap(kubeconfig, command string) (*meshconfig.MeshConfig, error) {
	client, err := createInterface(kubeconfig)
	if err != nil {
		return nil, err
	}

	meshConfigMap, err := client.CoreV1().ConfigMaps(istioNamespace).Get(context.TODO(), meshConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not read valid configmap %q from namespace %q: %v - "+
			"Use --meshConfigFile or re-run "+command+" with `-i <istioSystemNamespace> and ensure valid MeshConfig exists",
			meshConfigMapName, istioNamespace, err)
	}
	// values in the data are strings, while proto might use a
	// different data type.  therefore, we have to get a value by a
	// key
	configYaml, exists := meshConfigMap.Data[configMapKey]
	if !exists {
		return nil, fmt.Errorf("missing configuration map key %q", configMapKey)
	}
	cfg, err := mesh.ApplyMeshConfigDefaults(configYaml)
	if err != nil {
		err = multierror.Append(err, fmt.Errorf("istioctl version %s cannot parse mesh config.  Install istioctl from the latest Istio release",
			version.Info.Version))
	}
	return cfg, err
}

// grabs the raw values from the ConfigMap. These are encoded as JSON.
func getValuesFromConfigMap(kubeconfig string) (string, error) {
	client, err := createInterface(kubeconfig)
	if err != nil {
		return "", err
	}

	meshConfigMap, err := client.CoreV1().ConfigMaps(istioNamespace).Get(context.TODO(), injectConfigMapName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("could not find valid configmap %q from namespace  %q: %v - "+
			"Use --valuesFile or re-run kube-inject with `-i <istioSystemNamespace> and ensure istio-sidecar-injector configmap exists",
			injectConfigMapName, istioNamespace, err)
	}

	valuesData, exists := meshConfigMap.Data[valuesConfigMapKey]
	if !exists {
		return "", fmt.Errorf("missing configuration map key %q in %q",
			valuesConfigMapKey, injectConfigMapName)
	}

	return valuesData, nil
}

func getInjectConfigFromConfigMap(kubeconfig string) (inject.Templates, error) {
	client, err := createInterface(kubeconfig)
	if err != nil {
		return nil, err
	}

	meshConfigMap, err := client.CoreV1().ConfigMaps(istioNamespace).Get(context.TODO(), injectConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not find valid configmap %q from namespace  %q: %v - "+
			"Use --injectConfigFile or re-run kube-inject with `-i <istioSystemNamespace> and ensure istio-sidecar-injector configmap exists",
			injectConfigMapName, istioNamespace, err)
	}
	// values in the data are strings, while proto might use a
	// different data type.  therefore, we have to get a value by a
	// key
	injectData, exists := meshConfigMap.Data[injectConfigMapKey]
	if !exists {
		return nil, fmt.Errorf("missing configuration map key %q in %q",
			injectConfigMapKey, injectConfigMapName)
	}
	injectConfig, err := inject.UnmarshalConfig([]byte(injectData))
	if err != nil {
		return nil, fmt.Errorf("unable to convert data from configmap %q: %v",
			injectConfigMapName, err)
	}
	log.Debugf("using inject template from configmap %q", injectConfigMapName)
	return injectConfig.Templates, nil
}

func validateFlags() error {
	var err error
	if inFilename == "" {
		err = multierror.Append(err, errors.New("filename not specified (see --filename or -f)"))
	}
	if meshConfigFile == "" && meshConfigMapName == "" {
		err = multierror.Append(err, errors.New("--meshConfigFile or --meshConfigMapName must be set"))
	}
	return err
}

var (
	inFilename          string
	outFilename         string
	meshConfigFile      string
	meshConfigMapName   string
	valuesFile          string
	injectConfigFile    string
	injectConfigMapName string

	injectURL string
)

const (
	defaultMeshConfigMapName   = "istio"
	defaultInjectConfigMapName = "istio-sidecar-injector"
)

func injectCommand() *cobra.Command {
	var revision string

	injectCmd := &cobra.Command{
		Use:   "kube-inject",
		Short: "Inject Envoy sidecar into Kubernetes pod resources",
		Long: `
kube-inject manually injects the Envoy sidecar into Kubernetes
workloads. Unsupported resources are left unmodified so it is safe to
run kube-inject over a single file that contains multiple Service,
ConfigMap, Deployment, etc. definitions for a complex application. It's
best to do this when the resource is initially created.

k8s.io/docs/concepts/workloads/pods/pod-overview/#pod-templates is
updated for Job, DaemonSet, ReplicaSet, Pod and Deployment YAML resource
documents. Support for additional pod-based resource types can be
added as necessary.

The Istio project is continually evolving so the Istio sidecar
configuration may change unannounced. When in doubt re-run istioctl
kube-inject on deployments to get the most up-to-date changes.
`,
		Example: `  # Update resources on the fly before applying.
  kubectl apply -f <(istioctl kube-inject -f <resource.yaml>)

  # Create a persistent version of the deployment with Envoy sidecar injected.
  istioctl kube-inject -f deployment.yaml -o deployment-injected.yaml

  # Update an existing deployment.
  kubectl get deployment -o yaml | istioctl kube-inject -f - | kubectl apply -f -

  # Capture cluster configuration for later use with kube-inject
  kubectl -n istio-system get cm istio-sidecar-injector  -o jsonpath="{.data.config}" > /tmp/inj-template.tmpl
  kubectl -n istio-system get cm istio -o jsonpath="{.data.mesh}" > /tmp/mesh.yaml
  kubectl -n istio-system get cm istio-sidecar-injector -o jsonpath="{.data.values}" > /tmp/values.json

  # Use kube-inject based on captured configuration
  istioctl kube-inject -f samples/bookinfo/platform/kube/bookinfo.yaml \
    --injectConfigFile /tmp/inj-template.tmpl \
    --meshConfigFile /tmp/mesh.yaml \
    --valuesFile /tmp/values.json
`,
		RunE: func(c *cobra.Command, _ []string) (err error) {
			if err = validateFlags(); err != nil {
				return err
			}

			var reader io.Reader
			if inFilename == "-" {
				reader = os.Stdin
			} else {
				var in *os.File
				if in, err = os.Open(inFilename); err != nil {
					return err
				}
				reader = in
				defer func() {
					if errClose := in.Close(); errClose != nil {
						log.Errorf("Error: close file from %s, %s", inFilename, errClose)

						// don't overwrite the previous error
						if err == nil {
							err = errClose
						}
					}
				}()
			}

			var writer io.Writer
			if outFilename == "" {
				writer = c.OutOrStdout()
			} else {
				var out *os.File
				if out, err = os.Create(outFilename); err != nil {
					return err
				}
				writer = out
				defer func() {
					if errClose := out.Close(); errClose != nil {
						log.Errorf("Error: close file from %s, %s", outFilename, errClose)

						// don't overwrite the previous error
						if err == nil {
							err = errClose
						}
					}
				}()
			}

			return sendInjectRequest(reader, writer)

			var meshConfig *meshconfig.MeshConfig
			if meshConfigFile != "" {
				if meshConfig, err = mesh.ReadMeshConfig(meshConfigFile); err != nil {
					return err
				}
			} else {
				if meshConfig, err = getMeshConfigFromConfigMap(kubeconfig, "kube-inject"); err != nil {
					return err
				}
			}

			var sidecarTemplate inject.Templates
			if injectConfigFile != "" {
				injectionConfig, err := ioutil.ReadFile(injectConfigFile) // nolint: vetshadow
				if err != nil {
					return err
				}
				injectConfig, err := inject.UnmarshalConfig(injectionConfig)
				if err != nil {
					return multierror.Append(err, fmt.Errorf("loading --injectConfigFile"))
				}
				sidecarTemplate = injectConfig.Templates
			} else if sidecarTemplate, err = getInjectConfigFromConfigMap(kubeconfig); err != nil {
				return err
			}

			var valuesConfig string
			if valuesFile != "" {
				valuesConfigBytes, err := ioutil.ReadFile(valuesFile) // nolint: vetshadow
				if err != nil {
					return err
				}
				valuesConfig = string(valuesConfigBytes)
			} else if valuesConfig, err = getValuesFromConfigMap(kubeconfig); err != nil {
				return err
			}

			var warnings []string
			retval := inject.IntoResourceFile(sidecarTemplate, valuesConfig, revision, meshConfig,
				reader, writer, func(warning string) {
					warnings = append(warnings, warning)
				})
			if len(warnings) > 0 {
				fmt.Fprintln(c.ErrOrStderr())
			}
			for _, warning := range warnings {
				fmt.Fprintln(c.ErrOrStderr(), warning)
			}
			return retval
		},
		PersistentPreRunE: func(c *cobra.Command, args []string) error {
			// istioctl kube-inject is typically redirected to a .yaml file;
			// the default for log messages should be stderr, not stdout
			_ = c.Root().PersistentFlags().Set("log_target", "stderr")

			return c.Parent().PersistentPreRunE(c, args)
		},
	}

	injectCmd.PersistentFlags().StringVar(&meshConfigFile, "meshConfigFile", "",
		"Mesh configuration filename. Takes precedence over --meshConfigMapName if set")
	injectCmd.PersistentFlags().StringVar(&injectConfigFile, "injectConfigFile", "",
		"Injection configuration filename. Cannot be used with --injectConfigMapName")
	injectCmd.PersistentFlags().StringVar(&valuesFile, "valuesFile", "",
		"injection values configuration filename.")

	injectCmd.PersistentFlags().StringVarP(&inFilename, "filename", "f",
		"", "Input Kubernetes resource filename")
	injectCmd.PersistentFlags().StringVarP(&outFilename, "output", "o",
		"", "Modified output Kubernetes resource filename")

	injectCmd.PersistentFlags().StringVar(&meshConfigMapName, "meshConfigMapName", defaultMeshConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio mesh configuration, key should be %q", configMapKey))
	injectCmd.PersistentFlags().StringVar(&injectConfigMapName, "injectConfigMapName", defaultInjectConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio sidecar injection, key should be %q.", injectConfigMapKey))

	injectCmd.PersistentFlags().StringVar(&revision, "revision", "",
		"Control plane revision")

	return injectCmd
}

type nopInjector struct{}

func (nopInjector) Inject(pod *corev1.Pod) ([]byte, error) {
	return []byte("[]"), nil
}

type ExternalInjector struct {
	client   kube.ExtendedClient
	webhooks []admissionregistration.MutatingWebhookConfiguration
}

func (e ExternalInjector) Inject(pod *corev1.Pod) ([]byte, error) {
	cc, err := e.findClientConfig(pod)
	if err != nil {
		return nil, err
	}
	var address string
	if cc.URL != nil {
		address = *cc.URL
	}
	if cc.Service != nil {
		// TODO cache
		svc, err := e.client.CoreV1().Services(cc.Service.Namespace).Get(context.Background(), cc.Service.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		namespace, selector, err := polymorphichelpers.SelectorsForObject(svc)
		if err != nil {
			return nil, fmt.Errorf("cannot attach to %T: %v", svc, err)
		}
		sortBy := func(pods []*corev1.Pod) sort.Interface { return sort.Reverse(podutils.ActivePods(pods)) }
		pod, _, err := polymorphichelpers.GetFirstPod(e.client.CoreV1(), namespace, selector.String(), timeout, sortBy)
		if err != nil {
			return nil, err
		}
		// TODO derive from svc
		f, err := e.client.NewPortForwarder(pod.Name, pod.Namespace, "", 0, 15017)
		if err != nil {
			return nil, err
		}
		if err := f.Start(); err != nil {
			return nil, err
		}
		address = fmt.Sprintf("https://%s%s", f.Address(), *cc.Service.Path)
		defer func() {
			f.Close()
			f.WaitForStop()
		}()
	}
	client := http.Client{
		Timeout: time.Second * 5,
		Transport: &http.Transport{
			// TODO use cabundle
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	podBytes, err := json.Marshal(pod)
	if err != nil {
		return nil, err
	}
	rev := &admission.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: admission.SchemeGroupVersion.String(),
			Kind:       "AdmissionReview",
		},
		Request: &admission.AdmissionRequest{
			Object: runtime.RawExtension{Raw: podBytes},
			Kind: metav1.GroupVersionKind{
				Group:   admission.GroupName,
				Version: admission.SchemeGroupVersion.Version,
				Kind:    "AdmissionRequest",
			},
			Resource:           metav1.GroupVersionResource{},
			SubResource:        "",
			RequestKind:        nil,
			RequestResource:    nil,
			RequestSubResource: "",
			Name:               pod.Name,
			Namespace:          pod.Namespace,
		},
		Response: nil,
	}
	revBytes, err := json.Marshal(rev)
	if err != nil {
		return nil, err
	}
	resp, err := client.Post(address, "application/json", bytes.NewBuffer(revBytes))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	log.Errorf("howardjohn: %v", string(body))
	log.Errorf("howardjohn: %v", resp.StatusCode)
	return []byte("[]"), nil
}

func (e ExternalInjector) findClientConfig(pod *corev1.Pod) (*admissionregistration.WebhookClientConfig, error) {
	for _, mwh := range e.webhooks {
		if mwh.Name == "istio-sidecar-injector" {
			for _, wh := range mwh.Webhooks {
				if wh.Name == "sidecar-injector.istio.io" {
					return &wh.ClientConfig, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("failed to find matching mutating webhook configuration")
}

func sendInjectRequest(reader io.Reader, writer io.Writer) error {
	client, err := kube.NewExtendedClient(kube.BuildClientCmd(kubeconfig, configContext), "")
	if err != nil {
		return err
	}
	mwhs, err := client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to read mutating webhook configurations")
	}
	_ = mwhs

	injector := ExternalInjector{client, mwhs.Items}
	return inject.IntoResourceFileCall(reader, writer, injector)
}
