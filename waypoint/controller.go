package waypoint

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
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

//go:embed route-default.yaml
var routeDefaultTemplate string

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
	runGateway      = buildTemplate(gatewayTemplate)
	runServiceShim  = buildTemplate(serviceShimTemplate)
	runRoute        = buildTemplate(routeTemplate)
	runRouteDefault = buildTemplate(routeDefaultTemplate)
)

type Controller struct {
	client         kube.Client
	queue          controllers.Queue
	services       kclient.Client[*corev1.Service]
	secrets        kclient.Client[*corev1.Secret]
	gateways       kclient.Client[*gateway.Gateway]
	routes         kclient.Client[*gateway.HTTPRoute]
	namespaces     kclient.Client[*corev1.Namespace]
	gatewayClasses kclient.Client[*gateway.GatewayClass]
	gatewayClass   string

	patcher func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error
}

type ServiceShimInputs struct {
	Name      string
	Namespace string
	Ports     []int
	Address   string
}

type GatewayInputs struct {
	// Service name
	types.NamespacedName
	Ports []int
	Class string
}

type RouteDefaultInputs struct {
	// Service name
	types.NamespacedName
	Gateway *gateway.Gateway
	//Hostnames []string
	Ports []int
}

type RouteMirrorInputs struct {
	*gateway.HTTPRoute
	//Hostnames []string
	ServicePorts []int
}

const (
	domain      = "cluster.local"
	useWaypoint = "istio.io/use-waypoint"
)

func (c *Controller) Reconcile(key types.NamespacedName) error {
	ns := key.Namespace
	name := key.Name
	log := log.WithLabels("namespace", ns, "service", name)
	log.Infof("reconcile")

	svc := c.services.Get(name, ns)
	if svc == nil {
		log.Infof("service removed")
		return nil
	}

	nsObj := c.namespaces.Get(ns, "")
	if nsObj == nil {
		log.Infof("namespace does not exist")
		return nil
	}

	needsWaypoint := svc.Annotations[useWaypoint] != "" || nsObj.Annotations[useWaypoint] != ""
	if !needsWaypoint {
		log.Infof("service does not need waypoint, cleanup...")
		// TODO: cleanup
		return nil
	}

	gwi := GatewayInputs{
		NamespacedName: key,
		Ports:          extractServicePorts(svc),
		Class:          c.gatewayClass,
	}
	if err := c.runAndApply(runGateway, gwi); err != nil {
		return err
	}

	gwName := key.Name + "-waypoint"
	waypoint := c.gateways.Get(gwName, ns)
	if waypoint == nil {
		log.Infof("waypoint not yet found, maybe will be later")
		return nil
	}

	// Find all routes. If there are no routes, we will setup a default one
	routes := c.routesFor(key)
	if len(routes) == 0 {
		log.Infof("no routes for service, set up default")
		if err := c.runAndApply(runRouteDefault, routeDefaultInputs(waypoint, svc)); err != nil {
			return err
		}
	} else {
		grouped := slices.Group(routes, func(h *gateway.HTTPRoute) string {
			// TODO annotation, etc
			if strings.Contains(h.Name, "-waypoint-default-") {
				return "default"
			}
			if strings.Contains(h.Name, "-waypoint-mirror") {
				return "mirror"
			}
			return "user"
		})
		defaultRoutes := grouped["default"]
		userRoutes := grouped["user"]
		//mirrorRoutes := grouped["mirror"]
		if len(userRoutes) > 0 {
			for _, dr := range defaultRoutes {
				log.Infof("delete default route %v", dr.Name)
				if err := c.routes.Delete(dr.Name, dr.Namespace); err != nil {
					return err
				}
			}
		}
		// We do not need to prune mirrorRoutes as they use ownerReferences
		for _, r := range userRoutes {
			log.Infof("mirroring route %v", r.Name)
			if err := c.runAndApply(runRoute, routeMirrorInputs(r)); err != nil {
				return err
			}
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
				Ports:     extractServicePorts(svc),
				Address:   addr,
			}

			log.Infof("apply service shim")
			if err := c.runAndApply(runServiceShim, ssi); err != nil {
				return err
			}
		}
		svc := c.services.Get(gwName, ns)
		if svc != nil && svc.Spec.ClusterIP != "" {
			waypointAddress = svc.Spec.ClusterIP
		}
	}

	// For each service, mark the waypoint address it can be reached from
	patchBytes := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, "experimental.istio.io/waypoint", waypointAddress)
	if waypointAddress == "" {
		patchBytes = fmt.Sprintf(`{"metadata":{"annotations":{%q:null}}}`, "experimental.istio.io/waypoint")
	}
	log.Infof("waypoint address is %q", waypointAddress)
	if _, err := c.services.Patch(name, ns, types.MergePatchType, []byte(patchBytes)); err != nil {
		return err
	}

	return nil
}

func (c *Controller) runAndApply(tmpl func(d any) ([]string, error), input any) error {
	objs, err := tmpl(input)
	if err != nil {
		return fmt.Errorf("template %T failed: %v", input, err)
	}
	for _, obj := range objs {
		if err := c.apply(obj); err != nil {
			return fmt.Errorf("apply %T failed: %v", input, err)
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

func routeDefaultInputs(gw *gateway.Gateway, svc *corev1.Service) RouteDefaultInputs {
	return RouteDefaultInputs{
		NamespacedName: config.NamespacedName(svc),
		Gateway:        gw,
		Ports:          extractServicePorts(svc),
	}
}

func extractServicePorts(svc *corev1.Service) []int {
	ports := slices.Map(svc.Spec.Ports, func(e corev1.ServicePort) int {
		return int(e.Port)
	})
	// todo: dedupe ports? filter to only HTTP?
	return ports
}

func routeMirrorInputs(r *gateway.HTTPRoute) RouteMirrorInputs {
	hostnames := sets.New[string]()
	// Insert all specified hostnames directly (to support custom domains)
	for _, h := range r.Spec.Hostnames {
		hostnames.Insert(string(h))
	}
	ports := sets.New[int]()
	for _, p := range r.Spec.ParentRefs {
		if !isServiceReference(p) {
			continue
		}
		ports.Insert(int(ptr.OrDefault(p.Port, 0)))
		hostnames.Insert(string(p.Name))
		ns := string(ptr.OrDefault(p.Namespace, gateway.Namespace(r.Namespace)))
		hostnames.InsertAll(
			string(p.Name),
			fmt.Sprintf("%v.%s", p.Name, ns),
			fmt.Sprintf("%v.%s.svc", p.Name, ns),
			fmt.Sprintf("%v.%s.svc.%s", p.Name, ns, domain),
		)
	}
	return RouteMirrorInputs{
		HTTPRoute:    r,
		ServicePorts: sets.SortedList(ports),
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

func (c *Controller) routesFor(key types.NamespacedName) []*gateway.HTTPRoute {
	routes := c.routes.List(key.Namespace, klabels.Everything())
	return slices.FilterInPlace(routes, func(route *gateway.HTTPRoute) bool {
		for _, p := range route.Spec.ParentRefs {
			if !isServiceReference(p) {
				return false
			}
			if string(p.Name) != key.Name {
				return false
			}
			if p.Namespace != nil && string(*p.Namespace) != key.Namespace {
				return false
			}
			// It has a binding to this service.
			return true
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

	c.services = kclient.New[*corev1.Service](client)
	c.services.AddEventHandler(controllers.ObjectHandler(c.queue.AddObject))

	c.secrets = kclient.New[*corev1.Secret](client)
	//c.secrets.AddEventHandler(namespaceHandler)

	reconcileNamespace := controllers.ObjectHandler(func(o controllers.Object) {
		// TODO: this is very inefficient
		for _, svc := range c.services.List(o.GetNamespace(), klabels.Everything()) {
			c.queue.AddObject(svc)
		}
	})
	c.gateways = kclient.New[*gateway.Gateway](client)
	c.gateways.AddEventHandler(reconcileNamespace)

	c.gatewayClasses = kclient.New[*gateway.GatewayClass](client)

	c.routes = kclient.New[*gateway.HTTPRoute](client)
	c.routes.AddEventHandler(reconcileNamespace)

	c.namespaces = kclient.New[*corev1.Namespace](client)
	c.namespaces.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		for _, svc := range c.services.List(o.GetName(), klabels.Everything()) {
			c.queue.AddObject(svc)
		}
	}))

	return c
}
