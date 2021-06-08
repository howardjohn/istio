package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"istio.io/istio/pkg/config/schema/gvk"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	gateway "sigs.k8s.io/gateway-api/apis/v1alpha1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New())
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&GatewayReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Gateway"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Gateway")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
func init() {
	utilruntime.Must(gateway.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// GatewayReconciler reconciles a Gateway object
type GatewayReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("gateway", req.NamespacedName)
	log.WithValues("req", req).Info("reconcile")
	var gw gateway.Gateway
	if err := r.Get(ctx, req.NamespacedName, &gw); err != nil {
		log.Error(err, "unable to fetch Gateway")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: gvk.ServiceApisGateway.Version,
				Kind:       gvk.ServiceApisGateway.Kind,
				Name:       gw.Name,
				UID:        gw.UID,
			}},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: "TCP",
					Port:     80,
				},
			},
			Selector:                      nil,
			ClusterIP:                     "",
			ClusterIPs:                    nil,
			Type:                          "",
			ExternalIPs:                   nil,
			SessionAffinity:               "",
			LoadBalancerIP:                "",
			LoadBalancerSourceRanges:      nil,
			ExternalName:                  "",
			ExternalTrafficPolicy:         "",
			HealthCheckNodePort:           0,
			PublishNotReadyAddresses:      false,
			SessionAffinityConfig:         nil,
			TopologyKeys:                  nil,
			IPFamilies:                    nil,
			IPFamilyPolicy:                nil,
			AllocateLoadBalancerNodePorts: nil,
			LoadBalancerClass:             nil,
			InternalTrafficPolicy:         nil,
		},
	}
	res, err := controllerutil.CreateOrPatch(ctx, r.Client, svc, func() error {
		fmt.Println("in patch fn?")
		return nil
	})
	log.Error(err, "patch", "res", res)

	return ctrl.Result{}, nil
}

func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gateway.Gateway{}).
		Complete(r)
}
