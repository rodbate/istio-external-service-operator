package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IstioExternalServiceSpec defines the desired state of IstioExternalService
type IstioExternalServiceSpec struct {
	// Services istio external services
	// +kubebuilder:validation:MinItems=1
	Services []IstioExternalServiceEntry `json:"services"`
}

type IstioExternalServiceEntry struct {
	// Name istio external service name
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Endpoints istio external service endpoints
	// +kubebuilder:validation:MinItems=1
	Endpoints []IstioExternalServiceEndpoint `json:"endpoints"`
}

type IstioExternalServiceEndpoint struct {
	// Host istio external service hort or ip
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65536
	Port int32 `json:"port"`
	// HealthCheck
	// +kubebuilder:default:={timeoutSeconds: 1, periodSeconds: 10, successThreshold: 1, failureThreshold: 3}
	// +optional
	HealthCheck IstioExternalServiceHealthCheck `json:"healthCheck"`
}

type IstioExternalServiceHealthCheck struct {
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int32 `json:"timeoutSeconds"`
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	PeriodSeconds int32 `json:"periodSeconds"`
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	SuccessThreshold int32 `json:"successThreshold"`
	// +optional
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	FailureThreshold int32 `json:"failureThreshold"`
}

// IstioExternalServiceStatus defines the observed state of IstioExternalService
type IstioExternalServiceStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:categories=all
//+kubebuilder:resource:shortName=ies
//+kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`

// IstioExternalService is the Schema for the istioexternalservices API
type IstioExternalService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IstioExternalServiceSpec   `json:"spec,omitempty"`
	Status IstioExternalServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IstioExternalServiceList contains a list of IstioExternalService
type IstioExternalServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IstioExternalService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IstioExternalService{}, &IstioExternalServiceList{})
}
