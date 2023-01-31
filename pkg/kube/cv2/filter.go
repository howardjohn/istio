package cv2

import "istio.io/istio/pkg/config/labels"

type filter struct {
	name      string
	namespace string
	selects   map[string]string
	labels    map[string]string
	generic   func(any) bool
}

// TODO: namespace matters
func FilterName(name string) DepOption {
	return func(h *dependency) {
		h.filter.name = name
		h.key.name = name
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
