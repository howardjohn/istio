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

package settings

import (
	"testing"

	"istio.io/istio/pkg/config/constants"
)

func TestDefaultArgs(t *testing.T) {
	a := DefaultArgs()

	if a.MeshConfigFile != defaultMeshConfigFile {
		t.Fatalf("unexpected MeshConfigFile: %s", a.MeshConfigFile)
	}

	if a.DomainSuffix != constants.DefaultKubernetesDomain {
		t.Fatalf("unexpected DomainSuffix: %s", a.DomainSuffix)
	}

	if a.InitialConnectionWindowSize != 1024*1024*16 {
		t.Fatal("Default of InitialConnectionWindowSize should be 1024 * 1024 * 16")
	}
}

func TestArgs_String(t *testing.T) {
	a := DefaultArgs()
	// Should not crash
	_ = a.String()
}
