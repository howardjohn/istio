package outboundtrafficpolicy

import (
	"bytes"
	"html/template"
	"istio.io/api/mesh/v1alpha1"
	"reflect"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
)

const (
	ServiceEntry = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: https-service
spec:
  hosts:
  - istio.io
  location: MESH_EXTERNAL
  ports:
  - name: https
    number: 90
    protocol: HTTPS
  resolution: DNS
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: http
spec:
  hosts:
  - istio.io
  location: MESH_EXTERNAL
  ports:
  - name: http
    number: 80
    protocol: HTTP
  resolution: DNS
`
	SidecarScope = `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-to-service-entry-namespace
spec:
  egress:
  - hosts:
    - "{{.ImportNamespace}}/*"
    - "istio-system/*"
`
)

// We want to test "external" traffic. To do this without actually hitting an external endpoint,
// we can import only the service namespace, so the apps are not known
func createSidecarScope(t *testing.T, appsNamespace namespace.Instance, serviceNamespace namespace.Instance, g galley.Instance) {
	tmpl, err := template.New("SidecarScope").Parse(SidecarScope)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"ImportNamespace": serviceNamespace.Name()}); err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	if err := g.ApplyConfig(appsNamespace, buf.String()); err != nil {
		t.Errorf("failed to apply service entries: %v", err)
	}
}

// Expected is a map of protocol -> expected response codes
func RunExternalRequestTest(t *testing.T, expected map[model.Protocol][]string, mesh *v1alpha1.MeshConfig) {
	framework.
		NewTest(t).
		// RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			g := galley.NewOrFail(t, ctx, galley.Config{})
			p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g, MeshConfig: mesh})

			//appsNamespace := namespace.NewOrFail(t, ctx, "app", true)
			serviceNamespace := namespace.NewOrFail(t, ctx, "service", true)

			// External traffic should work even if we have service entries on the same ports
			if err := g.ApplyConfig(serviceNamespace, ServiceEntry); err != nil {
				t.Errorf("failed to apply service entries: %v", err)
			}

			instance := apps.NewOrFail(t, ctx, apps.Config{
				//Namespace: appsNamespace,
				Pilot:     p,
				Galley:    g,
				AppParams: []apps.AppParam{
					// {Name: "a"},
					// {Name: "b"},
				},
			})

			//createSidecarScope(t, appsNamespace, serviceNamespace, g)
			//time.Sleep(time.Second * 5)

			client := instance.GetAppOrFail("a", t)
			dest := instance.GetAppOrFail("b", t)

			cases := []struct {
				name     string
				protocol model.Protocol
			}{
				{
					"HTTP Traffic",
					model.ProtocolHTTP,
				},
				{
					"HTTPS Traffic",
					model.ProtocolHTTPS,
				},
			}
			for _, tc := range cases {
				for _, ep := range dest.EndpointsForProtocol(tc.protocol) {
					t.Run(tc.name, func(t *testing.T) {
						if tc.protocol == model.ProtocolHTTPS {
							t.Skip("https is currently not supported until #13386 is fixed")
						}
						//ep := dest.EndpointsForProtocol(tc.protocol)[0]
						//log.Errorf("howardjohn: %v", apps.ConfigDumpStr(dest))
						resp, err := client.Call(ep, apps.AppCallOptions{Timeout: time.Second})
						if err != nil {
							t.Errorf("call failed: %v", err)
						}
						codes := make([]string, 0, len(resp))
						for _, r := range resp {
							codes = append(codes, r.Code)
						}
						if !reflect.DeepEqual(codes, expected[tc.protocol]) {
							t.Errorf("got codes %v, expected %v", codes, expected[tc.protocol])
						}
					})
				}
			}
		})
}
