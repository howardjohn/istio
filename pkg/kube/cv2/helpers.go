package cv2

import (
	"fmt"
	"reflect"
	"sync"

	"k8s.io/client-go/tools/cache"

	"istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/kube/controllers"
)

func Filter[T any](data []T, f func(T) bool) []T {
	fltd := make([]T, 0, len(data))
	for _, e := range data {
		if f(e) {
			fltd = append(fltd, e)
		}
	}
	return fltd
}

func GetKey[O any](a O) Key[O] {
	ao, ok := any(a).(controllers.Object)
	if ok {
		k, _ := cache.MetaNamespaceKeyFunc(ao)
		return Key[O](k)
	}
	arn, ok := any(a).(resourceNamer)
	if ok {
		return Key[O](arn.ResourceName())
	}
	panic(fmt.Sprintf("Cannot get Key, got %T", a))
	return ""
}

func Map[T, U any](data []T, f func(T) U) []U {
	res := make([]U, 0, len(data))
	for _, e := range data {
		res = append(res, f(e))
	}
	return res
}

func MapFilter[T, U any](data []T, f func(T) *U) []U {
	res := make([]U, 0, len(data))
	for _, e := range data {
		r := f(e)
		if r != nil {
			res = append(res, *r)
		}
	}
	return res
}

func MapFilterError[T, U any](data []T, f func(T) (U, error)) []U {
	res := make([]U, 0, len(data))
	for _, e := range data {
		r, err := f(e)
		if err != nil {
			continue
		}
		res = append(res, r)
	}
	return res
}

func Ptr[T any](data T) *T {
	return &data
}

func Cast[I, O any](data I) O {
	return any(data).(O)
}

func AppendNonNil[T any](data []T, i *T) []T {
	if i != nil {
		data = append(data, *i)
	}
	return data
}

func HasName(a any) bool {
	_, ok := a.(controllers.Object)
	return ok
}

func GetName(a any) string {
	ak, ok := a.(controllers.Object)
	if ok {
		return ak.GetName()
	}
	log.Debugf("No Name, got %T", a)
	panic(fmt.Sprintf("No Name, got %T %+v", a, a))
	return ""
}

func GetNamespace(a any) string {
	ak, ok := a.(controllers.Object)
	if ok {
		return ak.GetNamespace()
	}
	panic(fmt.Sprintf("No Namespace, got %T", a))
	return ""
}

func GetLabels(a any) map[string]string {
	ak, ok := a.(controllers.Object)
	if ok {
		return ak.GetLabels()
	}
	panic(fmt.Sprintf("No Labels, got %T", a))
	return nil
}

func GetLabelSelector(a any) map[string]string {
	val := reflect.ValueOf(a)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	specField := val.FieldByName("Spec")
	if !specField.IsValid() {
		log.Debugf("obj %T has no Spec", a)
		return nil
	}

	labelsField := specField.FieldByName("Selector")
	if !labelsField.IsValid() {
		log.Debugf("obj %T has no Selector", a)
		return nil
	}

	switch s := labelsField.Interface().(type) {
	case *v1beta1.WorkloadSelector:
		return s.GetMatchLabels()
	case map[string]string:
		return s
	default:
		log.Debugf("obj %T has unknown Selector", s)
		return nil
	}
}

type mutexGuard[T any] struct {
	mu   sync.Mutex
	data T
}

func newMutex[T any](initial T) *mutexGuard[T] {
	return &mutexGuard[T]{data: initial}
}

func (m *mutexGuard[T]) With(f func(T)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(m.data)
}

func GetType[T any]() reflect.Type {
	t := reflect.TypeOf(*new(T))
	if t.Kind() != reflect.Struct {
		return t.Elem()
	}
	return t
}

func GetTypeOf(a any) reflect.Type {
	t := reflect.TypeOf(a)
	if t.Kind() != reflect.Struct {
		return t.Elem()
	}
	return t
}

func Flatten[T any](t **T) *T {
	if t == nil {
		return (*T)(nil)
	}
	return *t
}
