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

package cv2

import (
	"fmt"
	"strings"

	"istio.io/istio/pkg/config/labels"
)

type filter struct {
	name      string
	namespace string
	selects   map[string]string
	labels    map[string]string
	generic   func(any) bool
}

func (f filter) String() string {
	attrs := []string{}
	if f.name != "" {
		attrs = append(attrs, "name="+f.name)
	}
	if f.namespace != "" {
		attrs = append(attrs, "namespace="+f.namespace)
	}
	if f.selects != nil {
		attrs = append(attrs, fmt.Sprintf("selects=%v", f.selects))
	}
	if f.labels != nil {
		attrs = append(attrs, fmt.Sprintf("labels=%v", f.labels))
	}
	if f.generic != nil {
		attrs = append(attrs, "generic")
	}
	res := strings.Join(attrs, ",")
	return fmt.Sprintf("{%s}", res)
}

func FilterName(name, namespace string) DepOption {
	return func(h *dependency) {
		h.filter.name = name
		h.filter.namespace = namespace
		h.key.name = name
	}
}

func FilterNamespace(namespace string) DepOption {
	return func(h *dependency) {
		h.filter.namespace = namespace
	}
}

func FilterSelects(lbls map[string]string) DepOption {
	return func(h *dependency) {
		h.filter.selects = lbls
	}
}

func FilterLabel(lbls map[string]string) DepOption {
	return func(h *dependency) {
		h.filter.labels = lbls
	}
}

func FilterGeneric(f func(any) bool) DepOption {
	return func(h *dependency) {
		h.filter.generic = f
	}
}

func (f filter) Matches(object any) bool {
	if f.name != "" && f.name != GetName(object) {
		log.Debugf("no match name: %q vs %q", f.name, GetName(object))
		return false
	}
	if f.namespace != "" && f.namespace != GetNamespace(object) {
		log.Debugf("no match namespace: %q vs %q", f.namespace, GetNamespace(object))
		return false
	}
	if f.selects != nil && !labels.Instance(GetLabelSelector(object)).SubsetOf(f.selects) {
		log.Debugf("no match selects: %q vs %q", f.selects, GetLabelSelector(object))
		return false
	}
	if f.labels != nil && !labels.Instance(f.labels).SubsetOf(GetLabels(object)) {
		log.Debugf("no match labels: %q vs %q", f.labels, GetLabels(object))
		return false
	}
	if f.generic != nil && !f.generic(object) {
		log.Debugf("no match generic")
		return false
	}
	return true
}
