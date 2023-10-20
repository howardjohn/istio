package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
	examplev1 "istio.io/istio/servicev2/apis/v1"
)

//go:embed template.yaml
var yamlTemplate string

var runTemplate = func() func(d any) ([]string, error) {
	t := tmpl.MustParse(yamlTemplate)
	return func(d any) ([]string, error) {
		raw, err := tmpl.Execute(t, d)
		if err != nil {
			return nil, err
		}
		return yml.SplitString(raw), nil
	}
}()

func main() {
	// log.EnableKlogWithVerbosity(6)
	c, err := kube.NewDefaultClient()
	fatal(err)
	stop := make(chan struct{})
	ctl := NewController(c)
	go ctl.Run(stop)
	go c.RunAndWait(stop)
	cmd.WaitSignal(stop)
}

func fatal(err error) {
	if err != nil {
		panic(err)
	}
}

type Controller struct {
	client        kube.Client
	queue         controllers.Queue
	superServices kclient.Client[*examplev1.SuperService]
	services      kclient.Client[*corev1.Service]
	secrets       kclient.Client[*corev1.Secret]
	gateways      kclient.Client[*gateway.Gateway]
	namespaces    kclient.Client[*corev1.Namespace]

	patcher func(gvr schema.GroupVersionResource, parentName string, name string, namespace string, data []byte, subresources ...string) error
}

type Inputs struct {
	Name            string
	UID             types.UID
	Namespace       string
	Suffix          string
	GatewayHostname string
	Port            uint32
	Selector        map[string]string
	Shared          bool

	CACert, CAKey string
}

const (
	GatewayClass = "gke-l7-rilb"
	//Suffix       = "mesh.howardjohn.net"
	Suffix       = ""
)

func (c *Controller) Reconcile(key types.NamespacedName) error {
	log := log.WithLabels("key", key)

	ss := c.superServices.Get(key.Name, key.Namespace)
	if ss == nil {
		log.Debugf("super service no longer exists")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return nil
	}

	caCert, caKey := c.fetchCA()

	inputs := Inputs{
		Name:      key.Name,
		Namespace: key.Namespace,
		UID:       ss.UID,
		Suffix:    Suffix,
		Port:      ss.Spec.Ports[0].Port,
		Selector:  ss.Spec.Selector,
		Shared:    ss.Spec.Class != nil && (*ss.Spec.Class) == "Shared",
		CACert:    caCert,
		CAKey:     caKey,
	}

	gwName := key.Name
	if inputs.Shared {
		gwName = "all-services"
	}
	log.Infof("gateway name %v", gwName)
	gw := c.gateways.Get(gwName, key.Namespace)

	if gw != nil {
		for _, s := range gw.Status.Addresses {
			if s.Type != nil && *s.Type == gateway.HostnameAddressType {
				inputs.GatewayHostname = s.Value
				break
			}
		}
	}

	result, err := runTemplate(inputs)
	if err != nil {
		return fmt.Errorf("template: %v", err)
	}
	for _, t := range result {
		if err := c.apply(key, t); err != nil {
			return fmt.Errorf("apply failed: %v", err)
		}
	}

	if gw != nil {
		ss := &examplev1.SuperService{
			TypeMeta: metav1.TypeMeta{
				Kind:       gvk.SuperService.Kind,
				APIVersion: gvk.SuperService.Group + "/" + gvk.SuperService.Version,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      ss.Name,
				Namespace: ss.Namespace,
			},
		}
		for _, s := range gw.Status.Addresses {
			ss.Status.Addresses = append(ss.Status.Addresses, examplev1.SuperServiceStatusAddress{
				Type:  (*examplev1.AddressType)(s.Type),
				Value: s.Value,
			})
		}
		if len(ss.Status.Addresses) > 0 {
			if err := c.ApplyObject(ss, "status"); err != nil {
				return fmt.Errorf("update service status: %v", err)
			}
		}
	}
	log.Info("service updated")
	return nil
}

// ApplyObject renders an object with the given input and (server-side) applies the results to the cluster.
func (c *Controller) ApplyObject(obj controllers.Object, subresources ...string) error {
	j, err := config.ToJSON(obj)
	if err != nil {
		return err
	}
	m := map[string]any{}
	json.Unmarshal(j, &m)
	delete(m["metadata"].(map[string]any), "creationTimestamp")
	if len(subresources) == 1 && subresources[0] == "status" {
		delete(m, "spec")
	}
	j, _ = json.Marshal(m)

	gvr, err := controllers.ObjectToGVR(obj)
	if err != nil {
		return err
	}
	log.Debugf("applying %v", string(j))

	return c.patcher(gvr, obj.GetName(), obj.GetName(), obj.GetNamespace(), j, subresources...)
}

func (c *Controller) Run(stop <-chan struct{}) {
	kube.WaitForCacheSync(
		"deployment controller",
		stop,
		c.namespaces.HasSynced,
		c.services.HasSynced,
		c.secrets.HasSynced,
		c.gateways.HasSynced,
		c.superServices.HasSynced,
	)
	c.queue.Run(stop)
}

func (c *Controller) apply(parent types.NamespacedName, yml string) error {
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
	if err := c.patcher(gvr, parent.Name, us.GetName(), us.GetNamespace(), j); err != nil {
		return fmt.Errorf("patch %v/%v/%v: %v", us.GroupVersionKind(), us.GetNamespace(), us.GetName(), err)
	}
	return nil
}

func (c *Controller) fetchCA() (string, string) {
	s := c.secrets.Get("cacerts", "istio-system")
	if s == nil {
		return "", ""
	}
	return string(s.Data["ca-cert.pem"]), string(s.Data["ca-key.pem"])
}

func NewController(client kube.Client) *Controller {
	c := &Controller{
		client: client,
		patcher: func(gvr schema.GroupVersionResource, parentName string, name string, namespace string, data []byte, subresources ...string) error {
			c := client.Dynamic().Resource(gvr).Namespace(namespace)
			t := true
			fm := fmt.Sprintf("istio-%v", parentName) // Unique per name for shared type usage
			_, err := c.Patch(context.Background(), name, types.ApplyPatchType, data, metav1.PatchOptions{
				Force:        &t,
				FieldManager: fm,
			}, subresources...)
			return err
		},
	}
	c.queue = controllers.NewQueue("sevice controller",
		controllers.WithReconciler(c.Reconcile),
		controllers.WithMaxAttempts(5))

	// Set up a handler that will add the parent Gateway object onto the queue.
	// The queue will only handle Gateway objects; if child resources (Service, etc) are updated we re-add
	// the Gateway to the queue and reconcile the state of the world.
	parentHandler := controllers.ObjectHandler(controllers.EnqueueForParentHandler(c.queue, gvk.SuperService))

	c.services = kclient.New[*corev1.Service](client)
	c.services.AddEventHandler(parentHandler)
	c.secrets = kclient.New[*corev1.Secret](client)
	c.secrets.AddEventHandler(parentHandler)

	c.namespaces = kclient.New[*corev1.Namespace](client)
	c.namespaces.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		// TODO: make this more intelligent, checking if something we care about has changed
		// requeue this namespace
		for _, gw := range c.gateways.List(o.GetName(), klabels.Everything()) {
			c.queue.AddObject(gw)
		}
	}))

	c.gateways = kclient.New[*gateway.Gateway](client)
	c.gateways.AddEventHandler(parentHandler)

	c.superServices = kclient.New[*examplev1.SuperService](client)
	c.superServices.AddEventHandler(controllers.ObjectHandler(c.queue.AddObject))

	return c
}
