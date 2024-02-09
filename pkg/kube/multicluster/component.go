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

package multicluster

import (
	"sync"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/maps"
)

type Closer interface {
	Close()
}

type Component[T Closer] struct {
	mu       sync.RWMutex
	f        func(cluster *Cluster, stop <-chan struct{}) T
	clusters map[cluster.ID]T
}

func (m *Component[T]) ForCluster(clusterID cluster.ID) *T {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, f := m.clusters[clusterID]
	if !f {
		return nil
	}
	return &t
}

func (m *Component[T]) All() []T {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return maps.Values(m.clusters)
}

func (m *Component[T]) clusterAdded(cluster *Cluster, stop <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clusters[cluster.ID] = m.f(cluster, stop)
}

func (m *Component[T]) clusterUpdated(cluster *Cluster, stop <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// If there is an old one, close it
	if old, f := m.clusters[cluster.ID]; f {
		old.Close()
	}
	m.clusters[cluster.ID] = m.f(cluster, stop)
}

func (m *Component[T]) clusterDeleted(cluster cluster.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// If there is an old one, close it
	if old, f := m.clusters[cluster]; f {
		old.Close()
	}
	delete(m.clusters, cluster)
}
