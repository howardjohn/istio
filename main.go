package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"strings"

	ingress "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	serviceapis "sigs.k8s.io/gateway-api/apis/v1alpha1"
	"sigs.k8s.io/yaml"

	networking "istio.io/api/networking/v1alpha3"
	clientnetworkingalpha "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientnetworkingbeta "istio.io/client-go/pkg/apis/networking/v1beta1"
	clientsecurity "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/pkg/log"
)

func getScheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()
	if err := clientnetworkingalpha.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := clientnetworkingbeta.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := clientsecurity.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := scheme.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := serviceapis.AddToScheme(s); err != nil {
		return nil, err
	}
	return s, nil
}

func exit(e error) {
	if e == nil {
		return
	}
	log.Fatal(e)
}

func main() {
	o := log.DefaultOptions()
	o.OutputPaths = []string{"stderr"}
	log.Configure(o)
	scheme, err := getScheme()
	exit(err)
	data, err := ioutil.ReadAll(os.Stdin)
	exit(err)
	decoder := serializer.NewCodecFactory(scheme, serializer.EnableStrict).UniversalDeserializer()
	objs := []runtime.Object{}
	for _, item := range yml.SplitString(string(data)) {
		obj, _, err := decoder.Decode([]byte(item), nil, nil)
		if err == io.EOF {
			log.Infof("eof")
			break
		}
		exit(err)
		objs = append(objs, obj)
	}
	exitFailure := false
	gwHosts := map[string]string{}
	for _, obj := range objs {
		gw, ok := obj.(*clientnetworkingalpha.Gateway)
		if !ok {
			continue
		}
		if gw.Spec.GetSelector()["istio"] != "ingress-gke-system" {
			// Not meant for on-prem
			continue
		}
		for _, s := range gw.Spec.Servers {
			if s.GetPort().GetNumber() != 443 {
				if s.GetPort().GetNumber() != 80 {
					log.Warnf("Gateway %v server for hosts %v has unsupported port: %v", gw.Name, s.Hosts, s.GetPort().GetNumber())
					exitFailure = true
				}
				continue
			}
			if s.Tls == nil {
				log.Warnf("Gateway %v server for hosts %v has no TLS settings", gw.Name, s.Hosts, s.GetPort().GetNumber())
				exitFailure = true
				continue
			}
			if s.Tls.Mode != networking.ServerTLSSettings_SIMPLE {
				log.Warnf("Gateway %v server for hosts %v has unsupported TLS mode: %v", gw.Name, s.Hosts, s.Tls.Mode)
				exitFailure = true
				continue
			}
			// add more warnings
			for _, host := range s.Hosts {
				if _, f := gwHosts[host]; f {
					log.Warnf("Gateway %v server for host %v conflicts with another gateway", gw.Name, host)
					exitFailure = true
				}
				gwHosts[host] = s.Tls.CredentialName
			}
		}
	}
	registeredHosts := map[string]string{}
	for _, obj := range objs {
		ing, ok := obj.(*ingress.Ingress)
		if !ok {
			continue
		}
		wantHosts := sets.NewSet()
		for _, rule := range ing.Spec.Rules {
			wantHosts.Insert(rule.Host)
		}
		foundCreds := map[string]string{}
		for _, h := range wantHosts.UnsortedList() {
			if c, exact := gwHosts[h]; exact {
				foundCreds[h] = c
				continue
			}
			if h[0] != '*' {
				if c, exact := gwHosts[dropFirstLabel(h)]; exact {
					foundCreds[h] = c
					continue
				}
			}
		}
		for _, existing := range ing.Spec.TLS {
			for _, ehost := range existing.Hosts {
				if wantHosts.Contains(ehost) && foundCreds[ehost] != existing.SecretName {
					log.Warnf("existing TLS settings for Ingress %q host %q doesn't match expectation. Have %q, expected %q",
						ing.Name, ehost, existing.SecretName, foundCreds[ehost])
					exitFailure = true
				}
			}
		}
		tlsSetting := []ingress.IngressTLS{}
		hosts := wantHosts.UnsortedList()
		sort.Strings(hosts)
		for _, h := range hosts {
			if foundCreds[h] == "" {
				log.Warnf("failed to find a matching HTTPS credential for %v/%v; will be HTTP only", ing.Name, h)
				exitFailure = true
				continue
			}
			if existing, f := registeredHosts[h]; f {
				log.Warnf("conflicting TLS host for %q; host %q is the same as from %q; will be HTTP oonly", fmt.Sprintf("%s/%s", ing.Name, ing.Namespace), h, existing)
				exitFailure = true
				continue
			}
			registeredHosts[h] = fmt.Sprintf("%s/%s", ing.Name, ing.Namespace)
			tlsSetting = append(tlsSetting, ingress.IngressTLS{
				Hosts:      []string{h},
				SecretName: foundCreds[h],
			})
		}
		ing.Spec.TLS = tlsSetting
		b, err := yaml.Marshal(ing)
		exit(err)
		fmt.Println("---\n" + string(b))
	}
	if exitFailure {
		exit(fmt.Errorf("failures detected during execution"))
	}
}

func dropFirstLabel(s string) string {
	spl := strings.Split(s, ".")
	if len(spl) == 1 {
		return s
	}
	return "*." + strings.Join(spl[1:], ".")
}
