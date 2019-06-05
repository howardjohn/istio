package sidecarscope

import (
	"bytes"
	"fmt"
	"testing"
	"text/template"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	AppConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: app
  namespace: {{.AppNamespace}}
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 1.1.1.1
{{- end }}
---
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: sidecar
  namespace:  {{.AppNamespace}}
spec:
  egress:
    - hosts:
      - ./*
      - {{.IncludedNamespace}}/*
      - istio-system/*
`

	ExcludedConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: excluded
  namespace: {{.ExcludedNamespace}}
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: excluded.com
{{- else }}
  - address: 9.9.9.9
{{- end }}
`

	IncludedConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: included
  namespace: {{.IncludedNamespace}}
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: included.com
{{- else }}
  - address: 2.2.2.2
{{- end }}
`

	AppConfigListener = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: app-https
  namespace: {{.AppNamespace}}
spec:
  hosts:
  - {{.AppNamespace}}.cluster.local
  addresses:
  - 5.5.5.5
  ports:
  - number: 443
    name: https
    protocol: HTTPS
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 10.10.10.10
{{- end }}
`

	ExcludedConfigListener = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: excluded-https
  namespace: {{.ExcludedNamespace}}
spec:
  hosts:
  - {{.AppNamespace}}.cluster.local
  addresses:
  - 5.5.5.5
  ports:
  - number: 4430
    name: https
    protocol: HTTPS
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 10.10.10.10
{{- end }}
`
)

type Config struct {
	IncludedNamespace string
	ExcludedNamespace string
	AppNamespace      string
	Resolution        string
}

func setupTest(t *testing.T, ctx resource.Context, config Config) (pilot.Instance, *model.Proxy) {
	g := galley.NewOrFail(t, ctx, galley.Config{})
	p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})

	includedNamespace := namespace.NewOrFail(t, ctx, "included", true)
	excludedNamespace := namespace.NewOrFail(t, ctx, "excluded", true)
	appNamespace := namespace.NewOrFail(t, ctx, "app", true)

	config.IncludedNamespace = includedNamespace.Name()
	config.ExcludedNamespace = excludedNamespace.Name()
	config.AppNamespace = appNamespace.Name()

	// Apply all configs
	createConfig(t, g, config, AppConfig, appNamespace)
	time.Sleep(time.Second * 1)
	createConfig(t, g, config, AppConfigListener, appNamespace)
	time.Sleep(time.Second * 1)
	createConfig(t, g, config, ExcludedConfig, excludedNamespace)
	time.Sleep(time.Second * 1)
	createConfig(t, g, config, IncludedConfig, includedNamespace)
	time.Sleep(time.Second * 1)
	createConfig(t, g, config, ExcludedConfigListener, excludedNamespace)

	time.Sleep(time.Second * 2)

	nodeID := &model.Proxy{
		ClusterID:       "integration-test",
		ID:              fmt.Sprintf("app.%s", appNamespace.Name()),
		DNSDomain:       appNamespace.Name() + ".cluster.local",
		Type:            model.SidecarProxy,
		IPAddresses:     []string{"1.1.1.1"},
		ConfigNamespace: appNamespace.Name(),
	}
	return p, nodeID
}

func createConfig(t *testing.T, g galley.Instance, config Config, yaml string, namespace namespace.Instance) {
	tmpl, err := template.New("Config").Parse(yaml)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	if err := g.ApplyConfig(namespace, buf.String()); err != nil {
		t.Fatalf("failed to apply config: %v. Config: %v", err, buf.String())
	}
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("sidecar_scope_test", m).
		RequireEnvironment(environment.Native).
		Run()
}
