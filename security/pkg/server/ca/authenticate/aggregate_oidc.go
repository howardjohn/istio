// Copyright 2019 Istio Authors
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

package authenticate

import (
	"context"
	"fmt"
	"regexp"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/bootstrap/platform"
)

const (
	AutoIDTokenAuthenticatorType = "AutoIDTokenAuthenticator"
)

type AutoJwtAuthenticator struct {
	authenticators map[string]Authenticator
	// Regex to match trusted issuers
	issuerRegex *regexp.Regexp
	trustDomain string
	audience    string
}

var _ Authenticator = &AutoJwtAuthenticator{}

// newJwtAuthenticator is used when running istiod outside of a cluster, to validate the tokens using OIDC
// K8S is created with --service-account-issuer, service-account-signing-key-file and service-account-api-audiences
// which enable OIDC.
func NewAutoJwtAuthenticator(trustDomain, audience string) (*AutoJwtAuthenticator, error) {
	p := platform.NewGCP().Metadata()[platform.GCPProject]
	issuerRegex, err := regexp.Compile(fmt.Sprintf("https://container.googleapis.com/v1/projects/%s/.*", p))
	if err != nil {
		return nil, err
	}
	return &AutoJwtAuthenticator{
		issuerRegex:    issuerRegex,
		trustDomain:    trustDomain,
		audience:       audience,
		authenticators: map[string]Authenticator{},
	}, nil
}

// Authenticate - based on the old OIDC authenticator for mesh expansion.
func (j *AutoJwtAuthenticator) Authenticate(ctx context.Context) (*Caller, error) {
	tokenString, err := extractBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("ID token extraction error: %v", err)
	}

	token, err := ParseJwtToken(tokenString)
	if err != nil {
		return nil, fmt.Errorf("jwt token parsing: %v", err)
	}
	// We already trust this issuer
	if authenticator, f := j.authenticators[token.Iss]; f {
		return authenticator.Authenticate(ctx)
	}

	// We have never seen this issuer
	if err := j.AddAuthenticator(token.Iss, j.trustDomain, j.audience); err != nil {
		return nil, fmt.Errorf("failed to add issuer: %v", err)
	}
	// Check the new authenticator, if present
	if authenticator, f := j.authenticators[token.Iss]; f {
		return authenticator.Authenticate(ctx)
	}
	return nil, fmt.Errorf("no trusted issuer for found (have %v)", token.Iss)
}

func (j *AutoJwtAuthenticator) AddAuthenticator(issuer, trustDomain, audience string) error {
	if !j.issuerRegex.MatchString(issuer) {
		log.Warnf("Issuers %v is not a trusted issuer, cannot be used")
		return nil
	}
	auth, err := NewJwtAuthenticator(issuer, trustDomain, audience)
	if err != nil {
		return fmt.Errorf("add oidc authenticator: %v", err)
	}
	j.authenticators[issuer] = auth
	log.Infof("adding trusted issuer: %v", issuer)
	return nil
}

func (j AutoJwtAuthenticator) AuthenticatorType() string {
	return IDTokenAuthenticatorType
}
