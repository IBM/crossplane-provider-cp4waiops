/*
Copyright 2020 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"

	"k8s.io/apimachinery/pkg/runtime"
)

// MyTypeParameters are the configurable fields of a MyType.
type Cp4waiopsParameters struct {
	Catalogsource Catalogsource `json:"catalogsource"`
	InstallParams InstallParams `json:"installParams"`
}

type Catalogsource struct {
	Image   string `json:"image"`
	Channel string `json:"channel"`
}

type InstallParams struct {
	ImagePullSecret        string      `json:"imagePullSecret"`
	Namespace              string      `json:"namespace"`
	License                License     `json:"license"`
	PakModules             []PakModule `json:"pakModules,omitempty"`
	Size                   string      `json:"size"`
	StorageClass           string      `json:"storageClass"`
	StorageClassLargeBlock string      `json:"storageClassLargeBlock"`
}

type License struct {
	Accept bool `json:"accept"`
}

type PakModule struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled,omitempty"`
	// Config is the set of configurations for the operators in the module
	Configs []Config `json:"config,omitempty"`
}

// Config is the configuration of the specific operator in the module
type Config struct {
	// Name is the name of the Operator Config
	Name string `json:"name"`

	// Enabled is the installation mode of the operator
	Enabled *bool `json:"enabled,omitempty"`

	// Spec is the spec overrides to be passed into the operator
	Spec map[string]runtime.RawExtension `json:"spec,omitempty"`
}

// MyTypeObservation are the observable fields of a MyType.
type Cp4waiopsObservation struct {
	ObservableField string `json:"observableField,omitempty"`
}

// A MyTypeSpec defines the desired state of a MyType.
type Cp4waiopsSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       Cp4waiopsParameters `json:"forProvider"`
}

// A MyTypeStatus represents the observed state of a MyType.
type Cp4waiopsStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          Cp4waiopsObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true

// Cp4waiops describes all steps for installing aiops .
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="STATUS",type="string",JSONPath=".status.bindingPhase"
// +kubebuilder:printcolumn:name="STATE",type="string",JSONPath=".status.atProvider.state"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// Please replace `PROVIDER-NAME` with your actual provider name, like `aws`, `azure`, `gcp`, `alibaba`
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,cloudpak}
type Cp4waiops struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Cp4waiopsSpec   `json:"spec"`
	Status Cp4waiopsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Cp4waiopsList contains a list of Cp4waiops
type Cp4waiopsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cp4waiops `json:"items"`
}

