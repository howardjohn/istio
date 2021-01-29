// Copyright 2018 Istio Authors
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
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/pkg/log"

	"istio.io/istio/pkg/test/util/tmpl"
)

var (
	nsSetupBinDir          = "/opt/cni/bin"
	injectAnnotationKey    = annotation.SidecarInject.Name
	sidecarStatusKey       = annotation.SidecarStatus.Name
	interceptRuleMgrType   = defInterceptRuleMgrType
	loggingOptions         = log.DefaultOptions()
	podRetrievalMaxRetries = 30
	podRetrievalInterval   = 1 * time.Second
)

const ISTIOINIT = "istio-init"

// Kubernetes a K8s specific struct to hold config
type Kubernetes struct {
	K8sAPIRoot           string   `json:"k8s_api_root"`
	Kubeconfig           string   `json:"kubeconfig"`
	InterceptRuleMgrType string   `json:"intercept_type"`
	NodeName             string   `json:"node_name"`
	ExcludeNamespaces    []string `json:"exclude_namespaces"`
	CNIBinDir            string   `json:"cni_bin_dir"`
}

// PluginConf is whatever you expect your configuration json to be. This is whatever
// is passed in on stdin. Your plugin may wish to expose its functionality via
// runtime args, see CONVENTIONS.md in the CNI spec.
type PluginConf struct {
	types.NetConf // You may wish to not nest this type
	RuntimeConfig *struct {
		// SampleConfig map[string]interface{} `json:"sample"`
	} `json:"runtimeConfig"`

	// This is the previous result, when called in the context of a chained
	// plugin. Because this plugin supports multiple versions, we'll have to
	// parse this in two passes. If your plugin is not chained, this can be
	// removed (though you may wish to error if a non-chainable plugin is
	// chained.
	// If you need to modify the result before returning it, you will need
	// to actually convert it to a concrete versioned struct.
	RawPrevResult *map[string]interface{} `json:"prevResult"`
	PrevResult    *current.Result         `json:"-"`

	// Add plugin-specific flags here
	LogLevel   string     `json:"log_level"`
	Kubernetes Kubernetes `json:"kubernetes"`
}

// K8sArgs is the valid CNI_ARGS used for Kubernetes
// The field names need to match exact keys in kubelet args for unmarshalling
type K8sArgs struct {
	types.CommonArgs
	IP                         net.IP
	K8S_POD_NAME               types.UnmarshallableString // nolint: golint, stylecheck
	K8S_POD_NAMESPACE          types.UnmarshallableString // nolint: golint, stylecheck
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString // nolint: golint, stylecheck
}

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	conf := PluginConf{}

	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	// Parse previous result. Remove this if your plugin is not chained.
	if conf.RawPrevResult != nil {
		resultBytes, err := json.Marshal(conf.RawPrevResult)
		if err != nil {
			return nil, fmt.Errorf("could not serialize prevResult: %v", err)
		}
		res, err := version.NewResult(conf.CNIVersion, resultBytes)
		if err != nil {
			return nil, fmt.Errorf("could not parse prevResult: %v", err)
		}
		conf.RawPrevResult = nil
		conf.PrevResult, err = current.NewResultFromResult(res)
		if err != nil {
			return nil, fmt.Errorf("could not convert result to current version: %v", err)
		}
	}
	// End previous result parsing

	return &conf, nil
}

// cmdAdd is called for ADD requests
func cmdAdd(args *skel.CmdArgs) error {
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		log.Errorf("istio-cni cmdAdd parsing config %v", err)
		return err
	}
	var podIp string
	for _, ip := range conf.PrevResult.IPs {
		podIp = ip.Address.IP.String()
	}

	var loggedPrevResult interface{}
	if conf.PrevResult == nil {
		loggedPrevResult = "none"
	} else {
		loggedPrevResult = conf.PrevResult
	}

	log.WithLabels("version", conf.CNIVersion, "prevResult", loggedPrevResult).Info("CmdAdd config parsed")

	// Determine if running under k8s by checking the CNI args
	k8sArgs := K8sArgs{}
	if err := types.LoadArgs(args.Args, &k8sArgs); err != nil {
		return err
	}
	log.Infof("Getting identifiers with arguments: %s", args.Args)
	log.Infof("Loaded k8s arguments: %v", k8sArgs)
	if conf.Kubernetes.CNIBinDir != "" {
		nsSetupBinDir = conf.Kubernetes.CNIBinDir
	}
	if conf.Kubernetes.InterceptRuleMgrType != "" {
		interceptRuleMgrType = conf.Kubernetes.InterceptRuleMgrType
	}

	log.WithLabels("ContainerID", args.ContainerID, "Pod", string(k8sArgs.K8S_POD_NAME),
		"Namespace", string(k8sArgs.K8S_POD_NAMESPACE), "InterceptType", interceptRuleMgrType).Info("")

	// Check if the workload is running under Kubernetes.
	if string(k8sArgs.K8S_POD_NAMESPACE) != "" && string(k8sArgs.K8S_POD_NAME) != "" {
		excludePod := false
		for _, excludeNs := range conf.Kubernetes.ExcludeNamespaces {
			if string(k8sArgs.K8S_POD_NAMESPACE) == excludeNs {
				excludePod = true
				break
			}
		}
		if !excludePod {
			client, err := newKubeClient(*conf)
			if err != nil {
				return err
			}
			log.Debugf("Created Kubernetes client: %v", client)
			var containers []string
			var initContainersMap map[string]struct{}
			var annotations map[string]string
			var k8sErr error
			for attempt := 1; attempt <= podRetrievalMaxRetries; attempt++ {
				containers, initContainersMap, _, annotations, k8sErr = getKubePodInfo(client, string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE))
				if k8sErr == nil {
					break
				}
				log.WithLabels("err", k8sErr, "attempt", attempt).Warn("Waiting for pod metadata")
				time.Sleep(podRetrievalInterval)
			}
			if k8sErr != nil {
				log.WithLabels("err", k8sErr).Error("Failed to get pod data")
				return k8sErr
			}

			// Check if istio-init container is present; in that case exclude pod
			if _, present := initContainersMap[ISTIOINIT]; present {
				log.WithLabels(
					"pod", string(k8sArgs.K8S_POD_NAME),
					"namespace", string(k8sArgs.K8S_POD_NAMESPACE)).
					Info("Pod excluded due to being already injected with istio-init container")
				excludePod = true
			}

			log.Infof("Found containers %v", containers)
			log.Errorf("howardjohn: cni")
			if len(containers) >= 1 {
				log.Errorf("howardjohn: cni 1")
				log.WithLabels(
					"ContainerID", args.ContainerID,
					"netns", args.Netns,
					"pod", string(k8sArgs.K8S_POD_NAME),
					"Namespace", string(k8sArgs.K8S_POD_NAMESPACE),
					"annotations", annotations).
					Info("cni Checking annotations prior to redirect for Istio proxy")
				if val, ok := annotations[injectAnnotationKey]; ok {
					log.Infof("Pod %s contains inject annotation: %s", string(k8sArgs.K8S_POD_NAME), val)
					if injectEnabled, err := strconv.ParseBool(val); err == nil {
						if !injectEnabled {
							log.Infof("cni Pod excluded due to inject-disabled annotation")
							excludePod = true
						}
					}
				}
				log.Errorf("howardjohn: cni 2")
				//if _, ok := annotations[sidecarStatusKey]; !ok {
				//	log.Infof("Pod %s excluded due to not containing sidecar annotation", string(k8sArgs.K8S_POD_NAME))
				//	excludePod = true
				//}
				if strings.HasSuffix(string(k8sArgs.K8S_POD_NAME), "-proxy") || strings.HasSuffix(string(k8sArgs.K8S_POD_NAME), "-proxy-updated") {
					log.Errorf("howardjohn: exclude proxy pod %v", string(k8sArgs.K8S_POD_NAME))
					excludePod = true
				}
				if !excludePod {
					log.Errorf("howardjohn: cni 3")
					log.Infof("cni setting up redirect")
					if redirect, redirErr := NewRedirect(annotations); redirErr != nil {
						log.Errorf("cni Pod redirect failed due to bad params: %v", redirErr)
					} else {
						log.Infof("cni Redirect local ports: %v", redirect.includePorts)
						// Get the constructor for the configured type of InterceptRuleMgr
						interceptMgrCtor := GetInterceptRuleMgrCtor(interceptRuleMgrType)
						if interceptMgrCtor == nil {
							log.Errorf("Pod redirect failed due to unavailable InterceptRuleMgr of type %s",
								interceptRuleMgrType)
						} else {
							log.Errorf("howardjohn: cni running program!")
							rulesMgr := interceptMgrCtor()
							if err := rulesMgr.Program(args.Netns, redirect); err != nil {
								return err
							}
							pod, err := getPod(PodTemplate{
								Name:    string(k8sArgs.K8S_POD_NAME),
								Network: args.Netns,
								PodIp:   podIp,
							})
							if err != nil {
								log.Errorf("howardjohn: err creating pod spec: %v", err)
							}
							log.Errorf("howardjohn: created pod template %v with %v containers", pod.Name, len(pod.Spec.Containers))
							if _, err := client.CoreV1().Pods(string(k8sArgs.K8S_POD_NAMESPACE)).Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
								log.Errorf("howardjohn: failed to create pod: %v", err)
							}
							func() {
								attempts := 0
								for attempts < 10 {
									attempts++
									pod, err := client.CoreV1().Pods(string(k8sArgs.K8S_POD_NAMESPACE)).Get(context.Background(), string(k8sArgs.K8S_POD_NAME)+"-proxy", metav1.GetOptions{})
									if err != nil {
										log.Errorf("howardjohn: got err %v", err)
									}
									for _, c := range pod.Status.Conditions {
										if c.Type == v1.PodReady && c.Status == v1.ConditionTrue {
											log.Errorf("howardjohn: pod is ready!")
											return
										}
									}
									log.Errorf("howardjohn: pod not ready")
									time.Sleep(time.Second)
								}
								log.Errorf("howardjohn: timed out waiting for pod ready")
							}()

						}
					}
				}
			}
		} else {
			log.Infof("Pod excluded")
		}
	} else {
		log.Infof("No Kubernetes Data")
	}

	var result *current.Result
	if conf.PrevResult == nil {
		result = &current.Result{
			CNIVersion: current.ImplementedSpecVersion,
		}
	} else {
		// Pass through the result for the next plugin
		result = conf.PrevResult
	}
	return types.PrintResult(result, conf.CNIVersion)
}

func getPod(t PodTemplate) (*v1.Pod, error) {
	pyaml, err := tmpl.Evaluate(podTemplate, t)
	if err != nil {
		return nil, err
	}
	pod := &v1.Pod{}
	if err := yaml.Unmarshal([]byte(pyaml), pod); err != nil {
		return nil, err
	}
	return pod, nil
}

type PodTemplate struct {
	Name    string
	PodIp   string
	Network string
}

var podTemplate = `
apiVersion: v1
kind: Pod
metadata:
  name: {{.Name}}-proxy
spec:
  terminationGracePeriodSeconds: 2
  restartPolicy: OnFailure
  dnsPolicy: ClusterFirstWithHostNet
  containers:
  - command:
    - nsenter
    - --net=/net
    - pilot-agent
    - proxy
    - sidecar
    - --domain
    - $(POD_NAMESPACE).svc.cluster.local
    - --serviceCluster
    - proxy.$(POD_NAMESPACE)
    - --proxyLogLevel=warning
    - --proxyComponentLogLevel=misc:error
    - --trust-domain=cluster.local
    - --concurrency
    - "2"
    env:
    - name: INITIAL_EPOCH
      value: "-1"
    - name: JWT_POLICY
      value: third-party-jwt
    - name: PILOT_CERT_PROVIDER
      value: istiod
    - name: CA_ADDR
      value: istiod.istio-system.svc:15012
    - name: POD_NAME
      valueFrom:
        fieldRef:
          fieldPath: metadata.name
    - name: POD_NAMESPACE
      valueFrom:
        fieldRef:
          fieldPath: metadata.namespace
    - name: INSTANCE_IP
      valueFrom:
        fieldRef:
          fieldPath: status.podIP
    - name: SERVICE_ACCOUNT
      valueFrom:
        fieldRef:
          fieldPath: spec.serviceAccountName
    - name: HOST_IP
      valueFrom:
        fieldRef:
          fieldPath: status.hostIP
    - name: CANONICAL_SERVICE
      valueFrom:
        fieldRef:
          fieldPath: metadata.labels['service.istio.io/canonical-name']
    - name: CANONICAL_REVISION
      valueFrom:
        fieldRef:
          fieldPath: metadata.labels['service.istio.io/canonical-revision']
    - name: PROXY_CONFIG
      value: |
        {"proxyMetadata":{"DNS_AGENT":""},"meshId":"cluster.local"}
    - name: ISTIO_META_POD_PORTS
      value: |-
        [
        ]
    - name: ISTIO_META_APP_CONTAINERS
      value: proxy
    - name: ISTIO_META_CLUSTER_ID
      value: Kubernetes
    - name: ISTIO_META_INTERCEPTION_MODE
      value: REDIRECT
    - name: ISTIO_META_WORKLOAD_NAME
      value: proxy
    - name: ISTIO_META_OWNER
      value: kubernetes://apis/apps/v1/namespaces/default/deployments/proxy
    - name: ISTIO_META_MESH_ID
      value: cluster.local
    - name: DNS_AGENT
    image: localhost:5000/proxyv2:oop
    imagePullPolicy: Always
    name: istio-proxy
    resources:
      limits:
        cpu: "2"
        memory: 1Gi
      requests:
        cpu: 100m
        memory: 128Mi
    readinessProbe:
      failureThreshold: 90
      httpGet:
        host: {{.PodIp}}
        path: /healthz/ready
        port: 15021
        scheme: HTTP
      initialDelaySeconds: 1
      periodSeconds: 2
      successThreshold: 1
      timeoutSeconds: 1
    securityContext:
      privileged: true
      runAsGroup: 1337
    volumeMounts:
    - mountPath: /var/run/secrets/istio
      name: istiod-ca-cert
    - mountPath: /var/lib/istio/data
      name: istio-data
    - mountPath: /etc/istio/proxy
      name: istio-envoy
    - mountPath: /var/run/secrets/tokens
      name: istio-token
    - mountPath: /etc/istio/pod
      name: istio-podinfo
    - mountPath: /net
      name: net
  hostIPC: true
  volumes:
  - name: net
    hostPath:
      path: {{.Network}}
  - emptyDir:
      medium: Memory
    name: istio-envoy
  - emptyDir: {}
    name: istio-data
  - downwardAPI:
      items:
      - fieldRef:
          fieldPath: metadata.labels
        path: labels
      - fieldRef:
          fieldPath: metadata.annotations
        path: annotations
    name: istio-podinfo
  - name: istio-token
    projected:
      sources:
      - serviceAccountToken:
          audience: istio-ca
          expirationSeconds: 43200
          path: istio-token
  - configMap:
      name: istio-ca-root-cert
    name: istiod-ca-cert
`

func cmdGet(args *skel.CmdArgs) error {
	log.Info("cmdGet not implemented")
	// TODO: implement
	return fmt.Errorf("not implemented")
}

// cmdDel is called for DELETE requests
func cmdDel(args *skel.CmdArgs) error {
	log.Info("istio-cni cmdDel parsing config")
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}
	_ = conf
	k8sArgs := K8sArgs{}
	if err := types.LoadArgs(args.Args, &k8sArgs); err != nil {
		return err
	}
	log.Infof("Getting identifiers with arguments: %s", args.Args)
	log.Infof("Loaded k8s arguments: %v", k8sArgs)
	client, err := newKubeClient(*conf)
	if err != nil {
		return err
	}
	log.Debugf("Created Kubernetes client: %v", client)
	// Do your delete here
	if err := client.CoreV1().Pods(string(k8sArgs.K8S_POD_NAMESPACE)).Delete(context.Background(), string(k8sArgs.K8S_POD_NAME)+"-proxy", metav1.DeleteOptions{}); err != nil {
		log.Errorf("howardjohn: failed to delete pod: %v", err)
	}
	return nil
}

func main() {
	loggingOptions.OutputPaths = []string{"stderr"}
	loggingOptions.JSONEncoding = true
	if err := log.Configure(loggingOptions); err != nil {
		os.Exit(1)
	}
	// TODO: implement plugin version
	skel.PluginMain(cmdAdd, cmdGet, cmdDel, version.All, "istio-cni")
}
