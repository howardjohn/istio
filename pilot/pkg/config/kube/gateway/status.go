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

package gateway

import (
	"sync"

	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
)

type StatusRegistration = func(statusWriter status.Queue) krt.HandlerRegistration

// StatusCollections stores a variety of collections that can write status.
// These can be fed into a queue which can be dynamically changed (to handle leader election)
type StatusCollections struct {
	mu           sync.Mutex
	constructors []func(statusWriter status.Queue) krt.HandlerRegistration
	active       []krt.HandlerRegistration
	queue        status.Queue
}

func (s *StatusCollections) Register(sr StatusRegistration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.constructors = append(s.constructors, sr)
}

func (s *StatusCollections) UnsetQueue() {
	// Now we are disabled
	s.queue = nil
	for _, act := range s.active {
		act.UnregisterHandler()
	}
	s.active = nil
}

func (s *StatusCollections) SetQueue(queue status.Queue) []krt.Syncer {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Now we are enabled!
	s.queue = queue
	s.active = slices.Map(s.constructors, func(reg StatusRegistration) krt.HandlerRegistration {
		return reg(queue)
	})
	return slices.Map(s.active, func(e krt.HandlerRegistration) krt.Syncer {
		return e
	})
}

func registerStatus[I controllers.Object, IS any](c *Controller, statusCol krt.Collection[krt.ObjectWithStatus[I, IS]]) {
	reg := func(statusWriter status.Queue) krt.HandlerRegistration {
		h := statusCol.Register(func(o krt.Event[krt.ObjectWithStatus[I, IS]]) {
			l := o.Latest()
			EnqueueStatus(statusWriter, l.Obj, &l.Status)
		})
		return h
	}
	c.status.Register(reg)
}
