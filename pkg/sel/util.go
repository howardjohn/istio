package sel

import "reflect"

type Channel[T any] struct {
	Ch chan T
	F  func(t T)
}

func (c Channel[T]) channel() reflect.Value {
	return reflect.ValueOf(c.Ch)
}

func (c Channel[T]) apply(t any) {
	c.F(t.(T))
}

type ChannelType interface {
	channel() reflect.Value
	apply(t any)
}

func Random(f ...ChannelType) bool {
	cases := make([]reflect.SelectCase, 0, len(f))
	for _, c := range f {
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: c.channel(),
		})
	}
	chosen, v, ok := reflect.Select(cases)
	if !ok {
		return false
	}
	c := f[chosen]
	c.apply(v.Interface())
	return true
}

func Prioritized(f ...ChannelType) bool {
	cases := make([]reflect.SelectCase, 0, len(f)+1)
	cases = append(cases, reflect.SelectCase{Dir: reflect.SelectDefault})
	for _, c := range f {
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: c.channel(),
		})
	}
	for i := 1; i < len(cases); i++ {
		chosen, v, ok := reflect.Select(cases[0:i])
		if chosen == 0 {
			// default; continue
			continue
		}
		if !ok {
			return false
		}
		c := f[chosen-1]
		c.apply(v.Interface())
		return true
	}
	// None were ready, try randomly now
	chosen, v, ok := reflect.Select(cases[1:])
	if !ok {
		return false
	}
	c := f[chosen]
	c.apply(v.Interface())
	return true
}
