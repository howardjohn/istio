package cv2

import "istio.io/istio/pkg/config/labels"

type filter struct {
	name      string
	namespace string
	selects   map[string]string
}

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

func (f filter) Matches(object any) bool {
	if f.name != "" && f.name != GetName(object) {
		// log.Debugf("no match name: %q vs %q", f.name, GetName(object))
		return false
	} else {
		// log.Debugf("matches name: %q vs %q", f.name, GetName(object))
	}
	if f.namespace != "" && f.namespace != GetNamespace(object) {
		// log.Debugf("no match namespace: %q vs %q", f.namespace, GetNamespace(object))
		return false
	} else {
		// log.Debugf("matches namespace: %q vs %q", f.namespace, GetNamespace(object))
	}
	if f.selects != nil && !labels.Instance(f.selects).SubsetOf(GetLabelSelector(object)) {
		// log.Debugf("no match selects: %q vs %q", f.selects, GetLabelSelector(object))
		return false
	} else {
		// log.Debugf("matches selects: %q vs %q", f.selects, GetLabelSelector(object))
	}
	return true
}
