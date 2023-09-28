package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
	examplev1 "istio.io/istio/servicev2/apis/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"
	"strings"
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
	//log.EnableKlogWithVerbosity(6)
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
	gateways      kclient.Client[*gateway.Gateway]
	namespaces    kclient.Client[*corev1.Namespace]

	patcher func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error
}

type Inputs struct {
	Name string
	Namespace string
	Suffix string
	Port uint32
}

const (
	GatewayClass = "gke-l7-rilb"
	Suffix = "mesh.howardjohn.net"
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

	inputs := Inputs{
		Name:      key.Name,
		Namespace: key.Namespace,
		Suffix:    Suffix,
		Port:      ss.Spec.Ports[0].Port,
	}
	result, err := runTemplate(inputs)
	if err != nil {
		return fmt.Errorf("template: %v", err)
	}
	for _, t := range result {
		if err := c.apply(t); err != nil {
			return fmt.Errorf("apply failed: %v", err)
		}
	}
	return nil
}

func (c *Controller) Run(stop <-chan struct{}) {
	kube.WaitForCacheSync(
		"deployment controller",
		stop,
		c.namespaces.HasSynced,
		c.services.HasSynced,
		c.gateways.HasSynced,
		c.superServices.HasSynced,
	)
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

func NewController(client kube.Client) *Controller {
	c := &Controller{
		client: client,
		patcher: func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
			c := client.Dynamic().Resource(gvr).Namespace(namespace)
			t := true
			_, err := c.Patch(context.Background(), name, types.ApplyPatchType, data, metav1.PatchOptions{
				Force:        &t,
				FieldManager: constants.ManagedGatewayController,
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
