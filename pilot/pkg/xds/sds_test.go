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

package xds

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	credentials "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/spiffe"
)

func makeSecret(name string, data map[string]string) *corev1.Secret {
	bdata := map[string][]byte{}
	for k, v := range data {
		bdata[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "istio-system",
		},
		Data: bdata,
	}
}

var (
	genericCert = makeSecret("generic", map[string]string{
		credentials.GenericScrtCert: "generic-cert", credentials.GenericScrtKey: "generic-key",
	})
	genericMtlsCert = makeSecret("generic-mtls", map[string]string{
		credentials.GenericScrtCert: "generic-mtls-cert", credentials.GenericScrtKey: "generic-mtls-key", credentials.GenericScrtCaCert: "generic-mtls-ca",
	})
	genericMtlsCertSplit = makeSecret("generic-mtls-split", map[string]string{
		credentials.GenericScrtCert: "generic-mtls-split-cert", credentials.GenericScrtKey: "generic-mtls-split-key",
	})
	genericMtlsCertSplitCa = makeSecret("generic-mtls-split-cacert", map[string]string{
		credentials.GenericScrtCaCert: "generic-mtls-split-ca",
	})
)

func TestGenerate(t *testing.T) {
	type Expected struct {
		Key    string
		Cert   string
		CaCert string
	}
	allResources := []string{
		"kubernetes://generic", "kubernetes://generic-mtls", "kubernetes://generic-mtls-cacert",
		"kubernetes://generic-mtls-split", "kubernetes://generic-mtls-split-cacert",
	}
	cases := []struct {
		name                 string
		proxy                *model.Proxy
		resources            []string
		request              *model.PushRequest
		expect               map[string]Expected
		accessReviewResponse func(action k8stesting.Action) (bool, runtime.Object, error)
	}{
		{
			name:      "simple",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: []string{"kubernetes://generic"},
			request:   &model.PushRequest{Full: true},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  "generic-key",
					Cert: "generic-cert",
				},
			},
		},
		{
			name:      "sidecar",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}},
			resources: []string{"kubernetes://generic"},
			request:   &model.PushRequest{Full: true},
			expect:    map[string]Expected{},
		},
		{
			name:      "unauthenticated",
			proxy:     &model.Proxy{Type: model.Router},
			resources: []string{"kubernetes://generic"},
			request:   &model.PushRequest{Full: true},
			expect:    map[string]Expected{},
		},
		{
			name:      "multiple",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request:   &model.PushRequest{Full: true},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  "generic-key",
					Cert: "generic-cert",
				},
				"kubernetes://generic-mtls": {
					Key:  "generic-mtls-key",
					Cert: "generic-mtls-cert",
				},
				"kubernetes://generic-mtls-cacert": {
					CaCert: "generic-mtls-ca",
				},
				"kubernetes://generic-mtls-split": {
					Key:  "generic-mtls-split-key",
					Cert: "generic-mtls-split-cert",
				},
				"kubernetes://generic-mtls-split-cacert": {
					CaCert: "generic-mtls-split-ca",
				},
			},
		},
		{
			name:      "full push with updates",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: []string{"kubernetes://generic", "kubernetes://generic-mtls", "kubernetes://generic-mtls-cacert"},
			request: &model.PushRequest{Full: true, ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "generic-mtls", Namespace: "istio-system", Kind: gvk.Secret}: {},
			}},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  "generic-key",
					Cert: "generic-cert",
				},
				"kubernetes://generic-mtls": {
					Key:  "generic-mtls-key",
					Cert: "generic-mtls-cert",
				},
				"kubernetes://generic-mtls-cacert": {
					CaCert: "generic-mtls-ca",
				},
			},
		},
		{
			name:      "incremental push with updates",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request: &model.PushRequest{Full: false, ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "generic", Namespace: "istio-system", Kind: gvk.Secret}: {},
			}},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  "generic-key",
					Cert: "generic-cert",
				},
			},
		},
		{
			name:      "incremental push with updates - mtls",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request: &model.PushRequest{Full: false, ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "generic-mtls", Namespace: "istio-system", Kind: gvk.Secret}: {},
			}},
			expect: map[string]Expected{
				"kubernetes://generic-mtls": {
					Key:  "generic-mtls-key",
					Cert: "generic-mtls-cert",
				},
				"kubernetes://generic-mtls-cacert": {
					CaCert: "generic-mtls-ca",
				},
			},
		},
		{
			name:      "incremental push with updates - mtls split",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request: &model.PushRequest{Full: false, ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "generic-mtls-split", Namespace: "istio-system", Kind: gvk.Secret}: {},
			}},
			expect: map[string]Expected{
				"kubernetes://generic-mtls-split": {
					Key:  "generic-mtls-split-key",
					Cert: "generic-mtls-split-cert",
				},
				"kubernetes://generic-mtls-split-cacert": {
					CaCert: "generic-mtls-split-ca",
				},
			},
		},
		{
			name:      "incremental push with updates - mtls split ca update",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request: &model.PushRequest{Full: false, ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "generic-mtls-split-cacert", Namespace: "istio-system", Kind: gvk.Secret}: {},
			}},
			expect: map[string]Expected{
				"kubernetes://generic-mtls-split": {
					Key:  "generic-mtls-split-key",
					Cert: "generic-mtls-split-cert",
				},
				"kubernetes://generic-mtls-split-cacert": {
					CaCert: "generic-mtls-split-ca",
				},
			},
		},
		{
			// If an unknown resource is request, we return all the ones we do know about
			name:      "unknown",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: []string{"kubernetes://generic", "foo://invalid", "kubernetes://not-found"},
			request:   &model.PushRequest{Full: true},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  "generic-key",
					Cert: "generic-cert",
				},
			},
		},
		{
			// proxy without authorization
			name:      "unauthorized",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: []string{"kubernetes://generic"},
			request:   &model.PushRequest{Full: true},
			// Should get a response, but it will be empty
			expect: map[string]Expected{},
			accessReviewResponse: func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("not authorized")
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.proxy.Metadata == nil {
				tt.proxy.Metadata = &model.NodeMetadata{}
			}
			tt.proxy.Metadata.ClusterID = "Kubernetes"
			s := NewFakeDiscoveryServer(t, FakeOptions{
				KubernetesObjects: []runtime.Object{genericCert, genericMtlsCert, genericMtlsCertSplit, genericMtlsCertSplitCa},
			})
			cc := s.KubeClient().Kube().(*fake.Clientset)

			cc.Fake.Lock()
			if tt.accessReviewResponse != nil {
				cc.Fake.PrependReactor("create", "subjectaccessreviews", tt.accessReviewResponse)
			} else {
				credentials.DisableAuthorizationForTest(cc)
			}
			cc.Fake.Unlock()

			gen := s.Discovery.Generators[v3.SecretType]
			tt.request.Start = time.Now()
			secrets, _, _ := gen.Generate(s.SetupProxy(tt.proxy), s.PushContext(),
				&model.WatchedResource{ResourceNames: tt.resources}, tt.request)
			raw := xdstest.ExtractTLSSecrets(t, model.ResourcesToAny(secrets))

			got := map[string]Expected{}
			for _, scrt := range raw {
				got[scrt.Name] = Expected{
					Key:    string(scrt.GetTlsCertificate().GetPrivateKey().GetInlineBytes()),
					Cert:   string(scrt.GetTlsCertificate().GetCertificateChain().GetInlineBytes()),
					CaCert: string(scrt.GetValidationContext().GetTrustedCa().GetInlineBytes()),
				}
			}
			if diff := cmp.Diff(got, tt.expect); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

// TestCaching ensures we don't have cross-proxy cache generation issues. This is split from TestGenerate
// since it is order dependant.
// Regression test for https://github.com/istio/istio/issues/33368
func TestCaching(t *testing.T) {
	s := NewFakeDiscoveryServer(t, FakeOptions{
		KubernetesObjects: []runtime.Object{genericCert},
		KubeClientModifier: func(c *kube.Client) {
			cc := (*c).Kube().(*fake.Clientset)
			credentials.DisableAuthorizationForTest(cc)
		},
	})
	gen := s.Discovery.Generators[v3.SecretType]

	fullPush := &model.PushRequest{Full: true, Start: time.Now()}
	istiosystem := &model.Proxy{
		Metadata:         &model.NodeMetadata{ClusterID: "Kubernetes"},
		VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"},
		Type:             model.Router,
		ConfigNamespace:  "istio-system",
	}
	otherNamespace := &model.Proxy{
		Metadata:         &model.NodeMetadata{ClusterID: "Kubernetes"},
		VerifiedIdentity: &spiffe.Identity{Namespace: "other-namespace"},
		Type:             model.Router,
		ConfigNamespace:  "other-namespace",
	}

	secrets, _, _ := gen.Generate(s.SetupProxy(istiosystem), s.PushContext(),
		&model.WatchedResource{ResourceNames: []string{"kubernetes://generic"}}, fullPush)
	raw := xdstest.ExtractTLSSecrets(t, model.ResourcesToAny(secrets))
	if len(raw) != 1 {
		t.Fatalf("failed to get expected secrets for authorized proxy: %v", raw)
	}

	// We should not get secret returned, even though we are asking for the same one
	secrets, _, _ = gen.Generate(s.SetupProxy(otherNamespace), s.PushContext(),
		&model.WatchedResource{ResourceNames: []string{"kubernetes://generic"}}, fullPush)
	raw = xdstest.ExtractTLSSecrets(t, model.ResourcesToAny(secrets))
	if len(raw) != 0 {
		t.Fatalf("failed to get expected secrets for unauthorized proxy: %v", raw)
	}
}

func TestAtMostNJoin(t *testing.T) {
	tests := []struct {
		data  []string
		limit int
		want  string
	}{
		{
			[]string{"a", "b", "c"},
			2,
			"a, and 2 others",
		},
		{
			[]string{"a", "b", "c"},
			4,
			"a, b, c",
		},
		{
			[]string{"a", "b", "c"},
			1,
			"a, b, c",
		},
		{
			[]string{"a", "b", "c"},
			0,
			"a, b, c",
		},
		{
			[]string{},
			3,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s-%d", strings.Join(tt.data, "-"), tt.limit), func(t *testing.T) {
			if got := atMostNJoin(tt.data, tt.limit); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
