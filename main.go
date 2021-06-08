package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	appsv1 "k8s.io/api/apps/v1"
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

	ctrl.SetLogger(zap.New(zap.UseDevMode(true), zap.Level(zapcore.DebugLevel)))
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
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
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
	svcPorts := make([]corev1.ServicePort, 0, len(gw.Spec.Listeners))
	for i, l := range gw.Spec.Listeners {
		// TODO dedupe
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:     fmt.Sprintf("%s-%d", strings.ToLower(string(l.Protocol)), i),
			Protocol: "TCP",
			Port:     int32(l.Port),
		})
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
		},
	}
	if res, err := controllerutil.CreateOrPatch(ctx, r.Client, svc, func() error {
		if err := controllerutil.SetOwnerReference(&gw, svc, scheme); err != nil {
			return err
		}
		svc.Annotations = gw.Annotations
		svc.Labels = gw.Labels
		svc.Spec.Ports = svcPorts
		svc.Spec.Selector = map[string]string{
			"istio.io/gateway-name": gw.Name,
		}
		svc.Spec.Type = corev1.ServiceTypeLoadBalancer

		return nil
	}); err != nil {
		return ctrl.Result{}, err
	} else {
		log.WithValues("res", res).Info("service updated")
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
		},
	}
	if res, err := controllerutil.CreateOrPatch(ctx, r.Client, dep, func() error {
		if err := controllerutil.SetOwnerReference(&gw, dep, scheme); err != nil {
			return err
		}
		dep.Annotations = gw.Annotations
		dep.Labels = gw.Labels
		dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{
			"istio.io/gateway-name": gw.Name,
		}}
		dep.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					// TODO merge in gw annotations
					"inject.istio.io/templates": "gateway",
				},
				Labels: map[string]string{
					// TODO merge in gw labels, especially revision
					"sidecar.istio.io/inject": "true",
					"istio.io/gateway-name":   gw.Name,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "istio-proxy",
						Image: "auto",
					},
				},
			},
		}
		return nil
	}); err != nil {
		return ctrl.Result{}, err
	} else {
		log.WithValues("res", res).Info("deployment updated")
	}
	//if err := r.Patch(ctx, svc, client.Apply, client.ForceOwnership, client.FieldOwner("gateway-controller")); err != nil {
	//	return ctrl.Result{}, err
	//}
	log.Info("updated service")

	return ctrl.Result{}, nil
}

func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gateway.Gateway{}).
		Complete(r)
}
