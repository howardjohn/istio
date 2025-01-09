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

package meshwatcher

import (
	"os"
	"path"

	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/files"
	"istio.io/istio/pkg/log"
)

type MeshConfigSource = krt.Singleton[string]

func NewFileSource(fileWatcher filewatcher.FileWatcher, filename string, stop <-chan struct{}) (MeshConfigSource, error) {
	return files.NewSingleton[string](fileWatcher, filename, stop, func(filename string) (string, error) {
		b, err := os.ReadFile(filename)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}, krt.WithName("Mesh_File_"+path.Base(filename)), krt.WithStop(stop))
}

// NewCollection builds a new mesh config built by applying the provided sources.
// Sources are applied in order (example: default < sources[0] < sources[1]).
func NewCollection(stop <-chan struct{}, sources ...MeshConfigSource) krt.Singleton[MeshConfigResource] {
	return krt.NewSingleton[MeshConfigResource](
		func(ctx krt.HandlerContext) *MeshConfigResource {
			meshCfg := mesh.DefaultMeshConfig()

			for _, attempt := range sources {
				s := krt.FetchOne(ctx, attempt.AsCollection())
				if s == nil {
					// Source specified but not giving us any data
					continue
				}
				n, err := mesh.ApplyMeshConfig(*s, meshCfg)
				if err != nil {
					// For backwards compatibility, keep inconsistent behavior
					// TODO(https://github.com/istio/istio/issues/54615) align this.
					if len(sources) == 1 {
						log.Warnf("invalid mesh config, using last known state: %v", err)
						// We never want a nil mesh config. If it fails, we discard the result but allow falling back to the
						// default if there is no last known state.
						// We may consider failing hard on startup instead of silently ignoring errors.
						ctx.DiscardResult()
						return &MeshConfigResource{mesh.DefaultMeshConfig()}
					} else {
						log.Warnf("invalid mesh config, ignoring: %v", err)
						continue
					}
				}
				meshCfg = n
			}
			return &MeshConfigResource{meshCfg}
		}, krt.WithName("MeshConfig"), krt.WithStop(stop), krt.WithDebugging(krt.GlobalDebugHandler),
	)
}

// NewNetworksCollection builds a new meshnetworks config built by applying the provided sources.
// Sources are applied in order (example: default < sources[0] < sources[1]).
func NewNetworksCollection(stop <-chan struct{}, sources ...MeshConfigSource) krt.Singleton[MeshNetworksResource] {
	return krt.NewSingleton[MeshNetworksResource](
		func(ctx krt.HandlerContext) *MeshNetworksResource {
			for _, attempt := range sources {
				if s := krt.FetchOne(ctx, attempt.AsCollection()); s != nil {
					n, err := mesh.ParseMeshNetworks(*s)
					if err != nil {
						log.Warnf("invalid mesh networks, using last known state: %v", err)
						ctx.DiscardResult()
						return &MeshNetworksResource{mesh.DefaultMeshNetworks()}
					}
					return &MeshNetworksResource{n}
				}
			}
			return &MeshNetworksResource{nil}
		}, krt.WithName("MeshNetworks"), krt.WithStop(stop),
	)
}
