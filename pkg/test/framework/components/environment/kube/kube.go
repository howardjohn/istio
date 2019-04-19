//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/api"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"sigs.k8s.io/kind/pkg/exec"
)

// Environment is the implementation of a kubernetes environment. It implements environment.Environment,
// and also hosts publicly accessible methods that are specific to cluster environment.
type Environment struct {
	id resource.ID

	ctx api.Context
	*kube.Accessor
	s *Settings
}

func (e *Environment) Close() error {
	log.Errorf("howardjohn: Closing %v", e.s.KindCluster)
	if e.s.Kind {
		err := exec.Command("kind", "delete", "cluster", "--name", e.s.KindCluster).
			SetStderr(os.Stderr).
			SetStdout(os.Stdout).
			Run()
		if err != nil {
			return err
		}
	}
	return nil
}

var _ resource.Environment = &Environment{}

var _ io.Closer = &Environment{}

// New returns a new Kubernetes environment
func New(ctx api.Context) (resource.Environment, error) {
	s, err := newSettingsFromCommandline()
	if err != nil {
		return nil, err
	}

	scopes.CI.Infof("Test Framework Kubernetes environment Settings:\n%s", s)

	workDir, err := ctx.CreateTmpDirectory("env-kube")
	if err != nil {
		return nil, err
	}

	e := &Environment{
		ctx: ctx,
		s:   s,
	}
	e.id = ctx.TrackResource(e)

	if s.Kind {
		rand.Seed(time.Now().UnixNano())
		cluster := fmt.Sprintf("integration-%v", rand.Intn(20000))
		err := exec.Command("kind", "create", "cluster", "--name", cluster).
			SetStderr(os.Stderr).
			SetStdout(os.Stdout).
			Run()
		if err != nil {
			log.Errorf("howardjohn: failed to created kind: %v", err)
			return nil, err
		}
		//ctx := CreateKindCluster()
		//s.KubeConfig = ctx.KubeConfigPath()
		s.KindCluster = cluster
		s.KubeConfig = "/usr/local/google/home/howardjohn/.kube/kind-config-" + cluster
	}

	if e.Accessor, err = kube.NewAccessor(s.KubeConfig, workDir); err != nil {
		return nil, err
	}

	return e, nil
}

// EnvironmentName implements environment.Instance
func (e *Environment) EnvironmentName() environment.Name {
	return environment.Kube
}

// EnvironmentName implements environment.Instance
func (e *Environment) Case(name environment.Name, fn func()) {
	if name == e.EnvironmentName() {
		fn()
	}
}

// ID implements resource.Instance
func (e *Environment) ID() resource.ID {
	return e.id
}

func (e *Environment) Settings() *Settings {
	return e.s.clone()
}

// ApplyContents applies the given yaml contents to the namespace.
func (e *Environment) ApplyContents(namespace, yml string) error {
	_, err := e.Accessor.ApplyContents(namespace, yml)
	return err
}

// Applies the config in the given filename to the namespace.
func (e *Environment) Apply(namespace, ymlFile string) error {
	return e.Accessor.Apply(namespace, ymlFile)
}

// Deletes the given yaml contents from the namespace.
func (e *Environment) DeleteContents(namespace, yml string) error {
	return e.Accessor.DeleteContents(namespace, yml)
}

// Deletes the config in the given filename from the namespace.
func (e *Environment) Delete(namespace, ymlFile string) error {
	return e.Accessor.Delete(namespace, ymlFile)
}

func (e *Environment) DeployYaml(namespace, yamlFile string) (*deployment.Instance, error) {
	i := deployment.NewYamlDeployment(namespace, yamlFile)

	err := i.Deploy(e.Accessor, true)
	if err != nil {
		return nil, err
	}
	return i, nil
}
