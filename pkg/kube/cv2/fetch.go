package cv2

import (
	"fmt"
)

func FetchOne[T any](ctx HandlerContext, c Collection[T], opts ...DepOption) *T {
	res := Fetch[T](ctx, c, opts...)
	switch len(res) {
	case 0:
		return nil
	case 1:
		return &res[0]
	default:
		panic(fmt.Sprintf("FetchOne found for more than 1 item"))
	}
}

func Fetch[T any](ctx HandlerContext, c Collection[T], opts ...DepOption) []T {
	// First, set up the dependency. On first run, this will be new.
	// One subsequent runs, we just validate
	h := ctx.(depper)
	d := dependency{
		collection: eraseCollection(c),
		key:        depKey{dtype: GetType[T]()},
	}
	for _, o := range opts {
		o(&d)
	}
	if !h.registerDependency(d) {
		return nil
	}

	// Now we can do the real fetching
	var res []T
	for _, c := range c.List(d.filter.namespace) {
		c := c
		if d.filter.Matches(c) {
			res = append(res, c)
		}
	}
	log.WithLabels("key", d.key, "type", GetType[T](), "filter", d.filter, "size", len(res)).Debugf("Fetch")
	return res
}
