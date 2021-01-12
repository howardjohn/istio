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

// This is Google plugin of credentialfetcher.
package plugin

import (
	"io/ioutil"
	"strings"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/security"
)


// The plugin object.
type LocalJWTPlugin struct {
	// The location to save the identity token
	jwtPath string

	// identity provider
	identityProvider string
}

// CreateLocalJWTPlugin creates a Kubernetes credential fetcher, which reads a Kubernetes JWT token
func CreateLocalJWTPlugin(jwtPath, identityProvider string) *LocalJWTPlugin {
	p := &LocalJWTPlugin{
		jwtPath:          jwtPath,
		identityProvider: identityProvider,
	}
	return p
}

// GetPlatformCredential fetches the GCE VM identity jwt token from its metadata server,
// and write it to jwtPath. The local copy of the token in jwtPath is used by both
// Envoy STS client and istio agent to fetch certificate and access token.
// Note: this function only works in a GCE VM environment.
func (p *LocalJWTPlugin) GetPlatformCredential() (string, error) {
	if p.jwtPath == "" {
		return "", nil
	}
	tok, err := ioutil.ReadFile(p.jwtPath)
	if err != nil {
		log.Warnf("failed to fetch token from file: %v", err)
		return "", nil
	}
	return strings.TrimSpace(string(tok)), nil
}

// GetType returns credential fetcher type.
func (p *LocalJWTPlugin) GetType() string {
	return security.JWTCredentialFetcher
}

// GetIdentityProvider returns the name of the identity provider that can authenticate the workload credential.
func (p *LocalJWTPlugin) GetIdentityProvider() string {
	return p.identityProvider
}
