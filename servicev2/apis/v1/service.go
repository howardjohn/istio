// +k8s:deepcopy-gen=package
package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=s2
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Controller",type=string,JSONPath=`.spec.controllerName`
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="Description",type=string,JSONPath=`.spec.description`,priority=1
// +groupName=networking.example.io

//+kubebuilder:object:root=true
type SuperService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of SuperService.
	Spec SuperServiceSpec `json:"spec"`

	// Status defines the current state of SuperService.
	Status SuperServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true
type SuperServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SuperService `json:"items"`
}

type Port struct {
	Port uint32 `json:"port"`
	AppProtocol string `json:"appProtocol"`
}

type SuperServiceSpec struct {
	Selector map[string]string `json:"selector"`
	Ports []Port `json:"ports"`
}

type SuperServiceStatus struct {
	Addresses []SuperServiceStatusAddress `json:"addresses,omitempty"`
}

type SuperServiceStatusAddress struct {
	// Type of the address.
	//
	// +optional
	// +kubebuilder:default=IPAddress
	Type *AddressType `json:"type,omitempty"`

	// Value of the address. The validity of the values will depend
	// on the type and support by the controller.
	//
	// Examples: `1.2.3.4`, `128::1`, `my-ip-address`.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Value string `json:"value"`
}

// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=253
// +kubebuilder:validation:Pattern=`^Hostname|IPAddress|NamedAddress|[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*\/[A-Za-z0-9\/\-._~%!$&'()*+,;=:]+$`
type AddressType string
