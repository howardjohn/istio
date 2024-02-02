package waypoint

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"strings"

	"istio.io/istio/pkg/config/constants"
	kubecfg "istio.io/istio/pkg/config/kube"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/pkg/util/sets"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"
)

//go:embed gateway.yaml
var gatewayTemplate string

//go:embed service-shim.yaml
var serviceShimTemplate string

//go:embed route.yaml
var routeTemplate string

func buildTemplate(tm string) func(d any) ([]string, error) {
	t := tmpl.MustParse(tm)
	return func(d any) ([]string, error) {
		raw, err := tmpl.Execute(t, d)
		if err != nil {
			return nil, err
		}
		return yml.SplitString(raw), nil
	}
}

var (
	runGateway     = buildTemplate(gatewayTemplate)
	runServiceShim = buildTemplate(serviceShimTemplate)
	runRoute       = buildTemplate(routeTemplate)
)

type Controller struct {
	client         kube.Client
	queue          controllers.Queue
	services       kclient.Client[*corev1.Service]
	secrets        kclient.Client[*corev1.Secret]
	gateways       kclient.Client[*gateway.Gateway]
	waypoints      kclient.Client[*networkingv1alpha3.Waypoint]
	routes         kclient.Client[*gateway.HTTPRoute]
	namespaces     kclient.Client[*corev1.Namespace]
	gatewayClasses kclient.Client[*gateway.GatewayClass]
	gatewayClass   string

	patcher func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error
}

type ServiceShimInputs struct {
	Name      string
	Namespace string
	Ports     []int32
	Address   string
}

type GatewayInputs struct {
	Name      string
	Namespace string
	Ports     []int32
	Class     string
}

type RouteInputs struct {
	*gateway.HTTPRoute
	Hostnames []string
}

const (
	domain = "cluster.local"
)

func (c *Controller) Reconcile(key types.NamespacedName) error {
	ns := key.Name
	log := log.WithLabels("namespace", ns)
	log.Infof("reconcile")

	gwName := "waypoint"

	// Find all routes. If there are no routes, there is no waypoint
	routes := c.routesFor(ns)
	if len(routes) == 0 {
		log.Infof("no routes")
		return controllers.IgnoreNotFound(c.gateways.Delete(gwName, ns))
	}

	// Setup our primary waypoint gateway
	ports := c.findPorts(routes)
	gwi := GatewayInputs{
		Name:      gwName,
		Ports:     sets.SortedList(ports),
		Namespace: ns,
		Class:     c.gatewayClass,
	}
	gws, err := runGateway(gwi)
	if err != nil {
		return err
	}
	if err := c.apply(gws[0]); err != nil {
		return fmt.Errorf("gateway apply failed: %v", err)
	}

	// For each route, we need to make a mirror route that has the appropriate hostname matches and points to our Gateway.
	for _, r := range routes {
		routes, err := runRoute(routeInputs(r))
		if err != nil {
			return err
		}
		if err := c.apply(routes[0]); err != nil {
			return fmt.Errorf("route %v apply failed: %v", r.Name, err)
		}
	}

	// Our cilium redirection translates the target Service IP to another Service IP.
	// For external, the IP would be just an opaque IP to Cilium. We could probably make this work there, but for now workaround it.
	// Create a new Service for the waypoint and point it to the gateway address.
	var waypointAddress string
	gw := c.gateways.Get(gwName, ns)
	if gw != nil && len(gw.Status.Addresses) > 0 {
		waypointAddress = gw.Status.Addresses[0].Value
		if *gw.Status.Addresses[0].Type == gateway.HostnameAddressType {
			parts := strings.Split(waypointAddress, ".")
			// Its a FQDN... maybe its a Service we can resolve.
			// Otherwise this is not supported
			if len(parts) == 5 && parts[3] == "cluster" && parts[4] == "local" {
				svc := c.services.Get(parts[0], parts[1])
				if svc != nil && svc.Spec.ClusterIP != "" {
					waypointAddress = svc.Spec.ClusterIP
				}
			}
		}
	}
	if c.ExternalWaypoint() {
		if gw != nil && len(gw.Status.Addresses) > 0 {
			addr := gw.Status.Addresses[0].Value
			ssi := ServiceShimInputs{
				Name:      gwName,
				Namespace: ns,
				Ports:     sets.SortedList(ports),
				Address:   addr,
			}
			svcEp, err := runServiceShim(ssi)
			if err != nil {
				return err
			}
			if err := c.apply(svcEp[0]); err != nil {
				return fmt.Errorf("service apply failed: %v", err)
			}
			if err := c.apply(svcEp[1]); err != nil {
				return fmt.Errorf("endpoint apply failed: %v", err)
			}
		}
		svc := c.services.Get(gwName, ns)
		if svc != nil && svc.Spec.ClusterIP != "" {
			waypointAddress = svc.Spec.ClusterIP
		}
	}

	// For each service, mark the waypoint address it can be reached from
	if waypointAddress != "" {
		handled := sets.New[string]()
		for _, r := range routes {
			for _, p := range r.Spec.ParentRefs {
				if !isServiceReference(p) {
					continue
				}
				ns := string(ptr.OrDefault(p.Namespace, gateway.Namespace(r.Namespace)))
				svc := c.services.Get(string(p.Name), ns)
				handled.Insert(string(p.Name))
				if svc == nil {
					continue
				}
				if svc.Annotations == nil {
					svc.Annotations = map[string]string{}
				}
				svc.Annotations["experimental.istio.io/waypoint"] = waypointAddress
				// TODO move to patch
				// TODO: remove when its not needed anymore
				c.services.Update(svc)
			}
		}
		// Cleanup
		for _, svc := range c.services.List(ns, klabels.Everything()) {
			if handled.Contains(svc.Name) {
				continue
			}
			if _, f := svc.Annotations["experimental.istio.io/waypoint"]; f {
				delete(svc.Annotations, "experimental.istio.io/waypoint")
				// TODO move to patch
				c.services.Update(svc)
			}
		}
	}

	return nil
}

func (c *Controller) findPorts(routes []*gateway.HTTPRoute) sets.Set[int32] {
	ports := sets.New[int32]()
	for _, r := range routes {
		for _, pr := range r.Spec.ParentRefs {
			if !isServiceReference(pr) {
				continue
			}
			if pr.Port != nil {
				ports.Insert(int32(*pr.Port))
				continue
			}
			ns := string(ptr.OrDefault(pr.Namespace, gateway.Namespace(r.Namespace)))
			svc := c.services.Get(string(pr.Name), ns)
			if svc == nil {
				continue
			}
			for _, port := range svc.Spec.Ports {
				if kubecfg.ConvertProtocol(port.Port, port.Name, port.Protocol, port.AppProtocol).IsHTTP() {
					ports.Insert(port.Port)
				}
			}
		}
	}
	return ports
}

func routeInputs(r *gateway.HTTPRoute) RouteInputs {
	hostnames := sets.New[string]()
	// Insert all specified hostnames directly (to support custom domains)
	for _, h := range r.Spec.Hostnames {
		hostnames.Insert(string(h))
	}
	for _, p := range r.Spec.ParentRefs {
		if !isServiceReference(p) {
			continue
		}
		hostnames.Insert(string(p.Name))
		ns := string(ptr.OrDefault(p.Namespace, gateway.Namespace(r.Namespace)))
		hostnames.InsertAll(
			string(p.Name),
			fmt.Sprintf("%v.%s", p.Name, ns),
			fmt.Sprintf("%v.%s.svc", p.Name, ns),
			fmt.Sprintf("%v.%s.svc.%s", p.Name, ns, domain),
		)
	}
	return RouteInputs{
		HTTPRoute: r,
		Hostnames: sets.SortedList(hostnames),
	}
}

func (c *Controller) ExternalWaypoint() bool {
	return strings.HasPrefix(c.gatewayClass, "gke-")
}

func (c *Controller) Run(stop <-chan struct{}) {
	kube.WaitForCacheSync(
		"deployment controller",
		stop,
		c.namespaces.HasSynced,
		c.services.HasSynced,
		c.secrets.HasSynced,
		c.gateways.HasSynced,
		c.routes.HasSynced,
		c.gatewayClasses.HasSynced,
	)
	c.gatewayClass = c.detectClass()
	c.queue.Run(stop)
}

func (c *Controller) apply(yml string) error {
	data := map[string]any{}
	err := yaml.Unmarshal([]byte(yml), &data)
	if err != nil {
		return err
	}
	us := unstructured.Unstructured{Object: data}
	// set managed-by label
	clabel := strings.ReplaceAll(constants.ManagedGatewayController, "/", "-")
	err = unstructured.SetNestedField(us.Object, clabel, "metadata", "labels", constants.ManagedGatewayLabel)
	if err != nil {
		return err
	}
	gvr, err := controllers.UnstructuredToGVR(us)
	if err != nil {
		return err
	}
	j, err := json.Marshal(us.Object)
	if err != nil {
		return err
	}

	log.Debugf("applying %v", string(j))
	if err := c.patcher(gvr, us.GetName(), us.GetNamespace(), j); err != nil {
		return fmt.Errorf("patch %v/%v/%v: %v", us.GroupVersionKind(), us.GetNamespace(), us.GetName(), err)
	}
	return nil
}

func (c *Controller) routesFor(ns string) []*gateway.HTTPRoute {
	routes := c.routes.List(ns, klabels.Everything())
	return slices.FilterInPlace(routes, func(route *gateway.HTTPRoute) bool {
		for _, p := range route.Spec.ParentRefs {
			if isServiceReference(p) {
				return true
			}
		}
		return false
	})
}

func (c *Controller) detectClass() string {
	classes := sets.New[string]()
	for _, gc := range c.gatewayClasses.List(metav1.NamespaceAll, klabels.Everything()) {
		classes.Insert(gc.Name)
	}
	if classes.Contains("gke-l7-rilb") {
		return "gke-l7-rilb"
	}
	return "istio"
}

func isServiceReference(p k8sv1.ParentReference) bool {
	kind := ptr.OrDefault((*string)(p.Kind), gvk.KubernetesGateway.Kind)
	group := ptr.OrDefault((*string)(p.Group), gvk.KubernetesGateway.Group)
	if kind == gvk.Service.Kind && group == gvk.Service.Group {
		return true
	}
	return false
}

func NewController(client kube.Client) *Controller {
	c := &Controller{
		client: client,
		patcher: func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
			c := client.Dynamic().Resource(gvr).Namespace(namespace)
			t := true
			fm := "istio"
			_, err := c.Patch(context.Background(), name, types.ApplyPatchType, data, metav1.PatchOptions{
				Force:        &t,
				FieldManager: fm,
			}, subresources...)
			return err
		},
	}
	c.queue = controllers.NewQueue("waypoint controller",
		controllers.WithReconciler(c.Reconcile),
		controllers.WithMaxAttempts(5))

	c.waypoints = kclient.New[*networkingv1alpha3.Waypoint](client)
	c.waypoints.AddEventHandler(controllers.ObjectHandler(c.queue.AddObject))

	// Re-enqueue all waypoints in a namespace
	namespaceHandler := controllers.ObjectHandler(func(o controllers.Object) {
		for _, waypoint := range c.waypoints.List(o.GetNamespace(), klabels.Everything()) {
			c.queue.Add(config.NamespacedName(waypoint))
		}
	})

	c.services = kclient.New[*corev1.Service](client)
	c.services.AddEventHandler(namespaceHandler)
	c.secrets = kclient.New[*corev1.Secret](client)
	c.secrets.AddEventHandler(namespaceHandler)

	c.gateways = kclient.New[*gateway.Gateway](client)
	c.gateways.AddEventHandler(controllers.ObjectHandler(controllers.EnqueueForParentHandler(c.queue, gvk.Waypoint)))

	c.gatewayClasses = kclient.New[*gateway.GatewayClass](client)

	c.routes = kclient.New[*gateway.HTTPRoute](client)
	c.routes.AddEventHandler(namespaceHandler)

	c.namespaces = kclient.New[*corev1.Namespace](client)
	c.namespaces.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		for _, waypoint := range c.waypoints.List(o.GetName(), klabels.Everything()) {
			c.queue.Add(config.NamespacedName(waypoint))
		}
	}))

	return c
}
