package krt

import (
	"fmt"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"testing"
)

type Labeled struct {
	Labels map[string]string
}

func (l Labeled) GetLabels() map[string]string {
	return l.Labels
}

type SimplePod struct {
	Named
	Labeled
	IP string
}

func NewLabeled(n map[string]string) Labeled {
	return Labeled{n}
}

func SimplePodCollection(pods Collection[*corev1.Pod]) Collection[SimplePod] {
	return NewCollection(pods, func(ctx HandlerContext, i *corev1.Pod) *SimplePod {
		if i.Status.PodIP == "" {
			return nil
		}
		return &SimplePod{
			Named:   NewNamed(i),
			Labeled: NewLabeled(i.Labels),
			IP:      i.Status.PodIP,
		}
	})
}

type SimpleService struct {
	Named
	Selector map[string]string
}

func SimpleServiceCollection(services Collection[*corev1.Service]) Collection[SimpleService] {
	return NewCollection(services, func(ctx HandlerContext, i *corev1.Service) *SimpleService {
		return &SimpleService{
			Named:    NewNamed(i),
			Selector: i.Spec.Selector,
		}
	})
}

type SimpleEndpoint struct {
	Pod       string
	Service   string
	Namespace string
	IP        string
}

func (s SimpleEndpoint) ResourceName() string {
	return slices.Join("/", s.Namespace+"/"+s.Service+"/"+s.Pod)
}

func SimpleEndpointsCollection(pods Collection[SimplePod], services Collection[SimpleService]) Collection[SimpleEndpoint] {
	return NewManyCollection[SimpleService, SimpleEndpoint](services, func(ctx HandlerContext, svc SimpleService) []SimpleEndpoint {
		pods := Fetch(ctx, pods, FilterLabel(svc.Selector))
		return slices.Map(pods, func(pod SimplePod) SimpleEndpoint {
			return SimpleEndpoint{
				Pod:       pod.Name,
				Service:   svc.Name,
				Namespace: svc.Namespace,
				IP:        pod.IP,
			}
		})
	})
}

func TestAST(t *testing.T) {
	c := kube.NewFakeClient()
	pods := NewInformer[*corev1.Pod](c)
	services := NewInformer[*corev1.Service](c)
	SimplePods := SimplePodCollection(pods)
	SimpleServices := SimpleServiceCollection(services)
	SimpleEndpoints := SimpleEndpointsCollection(SimplePods, SimpleServices)
	pc := clienttest.Wrap(t, kclient.New[*corev1.Pod](c))
	sc := clienttest.Wrap(t, kclient.New[*corev1.Service](c))
	c.RunAndWait(test.NewStop(t))
	_ = SimpleEndpoints
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
	}
	pc.Create(pod)
	assert.Equal(t, fetcherSorted(SimpleEndpoints)(), nil)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "foo"}},
	}
	sc.Create(svc)
	assert.Equal(t, fetcherSorted(SimpleEndpoints)(), nil)

	pod.Status = corev1.PodStatus{PodIP: "1.2.3.4"}
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetcherSorted(SimpleEndpoints), []SimpleEndpoint{{pod.Name, svc.Name, pod.Namespace, "1.2.3.4"}})

	//Dump(SimpleEndpoints)
	DumpTree(SimpleEndpoints)
}

type Node struct {
	Id int
	Name string
}

func (n Node) String() string { return fmt.Sprintf("node%d", n.Id)}

func DumpTree(endpoints Collection[SimpleEndpoint]) {
	id := 0
	nodes := map[string]Node{}
	node := func(s string) Node {
		if v, f := nodes[s]; f {
			return v
		}
		id++
		n := Node{id, s}
		nodes[s] = n
		return n
	}
	mc := endpoints.(*manyCollection[SimpleService, SimpleEndpoint])
	root := node(mc.name)
	secondary := sets.New[string]()
	filters := map[string]sets.Set[string]{}
	for _, v := range mc.objectDependencies {
		for _, vs := range v {
			secondary.Insert(vs.collection.name)
			sets.InsertOrNew(filters, vs.collection.name, vs.filter.String())
		}
	}
	sb := &strings.Builder{}
	fmt.Fprintf(sb,"  %s\n", root)
	fmt.Fprintf(sb,"  %s-->%s\n", node(mc.parent.Name()), root)
	for s := range secondary {
		fmt.Fprintf(sb,"  %s-.%q.->%s\n", node(s), fmt.Sprint(filters[s].UnsortedList()), root)
	}
	fmt.Println("flowchart TD")
	for _, node := range nodes {
		fmt.Printf("  %s[%q]\n", node.String(), node.Name)
	}
	fmt.Print(sb.String())
	// TODO: recurse. Will require our type switch to not have the types in it though
}

func fetcherSorted[T ResourceNamer](c Collection[T]) func() []T {
	return func() []T {
		return slices.SortBy(c.List(""), func(t T) string {
			return t.ResourceName()
		})
	}
}