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

package krt

import (
	"fmt"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/ptr"
)

// NewStatusManyCollection builds a ManyCollection that outputs an additional *status* message about the original input.
// For example: with a Service input and []Endpoint output, I might report (ServiceStatus, []Endpoint) where ServiceStatus counts
// the number of attached endpoints.
// Two collections will be output: the status collection and the original output collection.
func NewStatusManyCollection[I, IStatus, O any](c Collection[I], hf TransformationMultiStatus[I, IStatus, O], opts ...CollectionOption) (Collection[ObjectWithStatus[I, IStatus]], Collection[O]) {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("NewStatusManyCollection[%v,%v,%v]", ptr.TypeName[I](), ptr.TypeName[IStatus](), ptr.TypeName[O]())
	}
	statusOpts := append(opts, WithName(o.name+"/status"))
	status := NewStaticCollection[ObjectWithStatus[I, IStatus]](nil, statusOpts...)
	// When input is deleted, the transformation function wouldn't run.
	// So we need to handle that explicitly
	cleanupOnRemoval := func(i []Event[I]) {
		for _, e := range i {
			if e.Event == controllers.EventDelete {
				status.DeleteObject(GetKey(e.Latest()))
			}
		}
	}
	primary := newManyCollection[I, O](c, func(ctx HandlerContext, i I) []O {
		st, objs := hf(ctx, i)
		// Create/delete our status objects
		if st == nil {
			status.DeleteObject(GetKey(i))
		} else {
			cs := ObjectWithStatus[I, IStatus]{
				Obj:    i,
				Status: *st,
			}
			status.ConditionalUpdateObject(cs)
		}
		return objs
	}, o, cleanupOnRemoval)

	return status, primary
}

func NewStatusCollection[I, IStatus, O any](c Collection[I], hf TransformationSingleStatus[I, IStatus, O], opts ...CollectionOption) (Collection[ObjectWithStatus[I, IStatus]], Collection[O]) {
	hm := func(ctx HandlerContext, i I) (*IStatus, []O) {
		status, res := hf(ctx, i)
		if res == nil {
			return status, nil
		}
		return status, []O{*res}
	}
	return NewStatusManyCollection(c, hm, opts...)
}

type ObjectWithStatus[I any, IStatus any] struct {
	Obj    I
	Status IStatus
}

func (c ObjectWithStatus[I, IStatus]) ResourceName() string {
	return GetKey(c.Obj)
}

func (c ObjectWithStatus[I, IStatus]) Equals(o ObjectWithStatus[I, IStatus]) bool {
	return equal(c.Status, o.Status)
}
