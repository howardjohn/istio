package v1alpha1

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/util/protomarshal"
)

type IstioOperatorSpec struct {
	// Path or name for the profile e.g.
	//
	// * minimal (looks in profiles dir for a file called minimal.yaml)
	// * /tmp/istio/install/values/custom/custom-install.yaml (local file path)
	//
	// default profile is used if this field is unset.
	Profile string `json:"profile,omitempty"`
	// Path for the install package. e.g.
	//
	// * /tmp/istio-installer/nightly (local file path)
	InstallPackagePath string `json:"installPackagePath,omitempty"`
	// Root for docker image paths e.g. `docker.io/istio`
	Hub string `json:"hub,omitempty"`
	// Version tag for docker images e.g. `1.7.2`
	Tag string `json:"tag,omitempty"`
	// Namespace to install control plane resources into. If unset, Istio will be installed into the same namespace
	// as the `IstioOperator` CR. You must also set `values.global.istioNamespace` if you wish to install Istio in
	// a custom namespace.
	// If you have enabled CNI, you must  exclude this namespace by adding it to the list `values.cni.excludeNamespaces`.
	Namespace string `json:"namespace,omitempty"`
	// Identify the revision this installation is associated with.
	// This option is currently experimental.
	Revision string `json:"revision,omitempty"`
	// Compatibility version allows configuring Istio to behave like an older version by tuning various settings to align with a
	// previous versions defaults. This accepts a `major.minor` format, such as `1.23`.
	// This option is currently experimental.
	CompatibilityVersion string `json:"compatibilityVersion,omitempty"`
	// Config used by control plane components internally.
	MeshConfig *MeshConfig `json:"meshConfig,omitempty"`
	// Kubernetes resource settings, enablement and component-specific settings that are not internal to the
	// component.
	Components IstioComponentSetSpec `json:"components,omitempty"`
	// Overrides for default `values.yaml`. This is a validated pass-through to Helm templates.
	// See the [Helm installation options](https://istio.io/v1.5/docs/reference/config/installation-options/) for schema details.
	// Anything that is available in `IstioOperatorSpec` should be set above rather than using the passthrough. This
	// includes Kubernetes resource settings for components in `KubernetesResourcesSpec`.
	Values *Values `json:"values,omitempty"`
	// Unvalidated overrides for default `values.yaml`. Used for custom templates where new parameters are added.
	UnvalidatedValues map[string]any `json:"unvalidatedValues,omitempty"`
}

type IstioComponentSetSpec struct {
	Base    *BaseComponentSpec `json:"base,omitempty"`
	Pilot   *ComponentSpec     `json:"pilot,omitempty"`
	Cni     *ComponentSpec     `json:"cni,omitempty"`
	Ztunnel *ComponentSpec     `json:"ztunnel,omitempty"`
	// Remote cluster using an external control plane.
	IstiodRemote    *ComponentSpec `json:"istiodRemote,omitempty"`
	IngressGateways []*GatewaySpec `json:"ingressGateways,omitempty"`
	EgressGateways  []*GatewaySpec `json:"egressGateways,omitempty"`
}

type ComponentSpec struct {
	// Selects whether this component is installed.
	Enabled *BoolValue `json:"enabled,omitempty"`
	// Namespace for the component.
	Namespace string `json:"namespace,omitempty"`
	// Hub for the component (overrides top level hub setting).
	Hub string `json:"hub,omitempty"`
	// Tag for the component (overrides top level tag setting).
	Tag string `json:"tag,omitempty"`
	// Arbitrary install time configuration for the component.
	Spec *structpb.Struct `json:"spec,omitempty"`
	// Kubernetes resource spec.
	K8S *KubernetesResourcesSpec `json:"k8s,omitempty"`
}

type BaseComponentSpec struct {
	// Selects whether this component is installed.
	Enabled *BoolValue `json:"enabled,omitempty"`
	// Kubernetes resource spec.
	K8S *KubernetesResourcesSpec `json:"k8s,omitempty"`
}

type GatewaySpec struct {
	// Selects whether this gateway is installed.
	Enabled *BoolValue `json:"enabled,omitempty"`
	// Namespace for the gateway.
	Namespace string `json:"namespace,omitempty"`
	// Name for the gateway.
	Name string `json:"name,omitempty"`
	// Labels for the gateway.
	Label map[string]string `json:"label,omitempty"`
	// Hub for the component (overrides top level hub setting).
	Hub string `json:"hub,omitempty"`
	// Tag for the component (overrides top level tag setting).
	Tag string `json:"tag,omitempty"`
	// Kubernetes resource spec.
	K8S *KubernetesResourcesSpec `json:"k8s,omitempty"`
}

// KubernetesResourcesSpec is a common set of Kubernetes resource configs for components.
type KubernetesResourcesSpec struct {
	// Kubernetes affinity.
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// Deployment environment variables.
	Env []*corev1.EnvVar `json:"env,omitempty"`
	// Kubernetes HorizontalPodAutoscaler settings.
	HpaSpec *autoscaling.HorizontalPodAutoscalerSpec `json:"hpaSpec,omitempty"`
	// Kubernetes imagePullPolicy.
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`
	// Kubernetes nodeSelector.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Kubernetes PodDisruptionBudget settings.
	PodDisruptionBudget *policy.PodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// Kubernetes pod annotations.
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`
	// Kubernetes priorityClassName. Default for all resources unless overridden.
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// Kubernetes readinessProbe settings.
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
	// Kubernetes Deployment replicas setting.
	ReplicaCount uint32 `json:"replicaCount,omitempty"`
	// Kubernetes resources settings.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// Kubernetes Service settings.
	Service *corev1.ServiceSpec `json:"service,omitempty"`
	// Kubernetes deployment strategy.
	Strategy *appsv1.DeploymentStrategy `json:"strategy,omitempty"`
	// Kubernetes toleration
	Tolerations []*corev1.Toleration `json:"tolerations,omitempty"`
	// Kubernetes service annotations.
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`
	// Kubernetes pod security context
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`
	// Kubernetes volumes
	// Volumes defines the collection of Volume to inject into the pod.
	Volumes []*corev1.Volume `json:"volumes,omitempty"`
	// Kubernetes volumeMounts
	// VolumeMounts defines the collection of VolumeMount to inject into containers.
	VolumeMounts []*corev1.VolumeMount `json:"volumeMounts,omitempty"`
	// Overlays for Kubernetes resources in rendered manifests.
	Overlays []*K8SObjectOverlay `json:"overlays,omitempty"`
}

// Patch for an existing Kubernetes resource.
type K8SObjectOverlay struct {
	// Resource API version.
	ApiVersion string `json:"apiVersion,omitempty"`
	// Resource kind.
	Kind string `json:"kind,omitempty"`
	// Name of resource.
	// Namespace is always the component namespace.
	Name string `json:"name,omitempty"`
	// List of patches to apply to resource.
	Patches []*K8SObjectOverlay_PathValue `json:"patches,omitempty"`
}

type K8SObjectOverlay_PathValue struct {
	// Path of the form a.[key1:value1].b.[:value2]
	// Where [key1:value1] is a selector for a key-value pair to identify a list element and [:value] is a value
	// selector to identify a list element in a leaf list.
	// All path intermediate nodes must exist.
	Path string `json:"path,omitempty"`
	// Value to add, delete or replace.
	// For add, the path should be a new leaf.
	// For delete, value should be unset.
	// For replace, path should reference an existing node.
	// All values are strings but are converted into appropriate type based on schema.
	Value *structpb.Value `json:"value,omitempty"`
}

type BoolValue struct {
	bool
}

func (b *BoolValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.GetValue())
}

func (b *BoolValue) UnmarshalJSON(bytes []byte) error {
	bb := false
	if err := json.Unmarshal(bytes, &bb); err != nil {
		return err
	}
	*b = BoolValue{bb}
	return nil
}

func (b *BoolValue) GetValue() bool {
	if b == nil {
		return false
	}
	return b.bool
}

var (
	_ json.Unmarshaler = &BoolValue{}
	_ json.Marshaler   = &BoolValue{}
)

type MeshConfig struct {
	*meshconfig.MeshConfig
}

func (m MeshConfig) Inner() *meshconfig.MeshConfig {
	return m.MeshConfig
}

func (m *MeshConfig) UnmarshalJSON(bytes []byte) error {
	// ApplyMeshConfigWithoutValidation allows unknown fields, so we first check for unknown fields
	if err := protomarshal.ApplyYAMLStrict(string(bytes), mesh.DefaultMeshConfig()); err != nil {
		return fmt.Errorf("failed to unmarshal mesh config: %v", err)
	}
	mc, err := mesh.ApplyMeshConfigWithoutValidation(string(bytes), mesh.DefaultMeshConfig())
	if err != nil {
		return err
	}
	*m = MeshConfig{MeshConfig: mc}
	return nil
}

func (m *MeshConfig) MarshalJSON() ([]byte, error) {
	if m.MeshConfig == nil {
		return nil, nil
	}
	return protomarshal.Marshal(m.MeshConfig)
}

var (
	_ json.Unmarshaler = &MeshConfig{}
	_ json.Marshaler   = &MeshConfig{}
)
