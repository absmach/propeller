// Package v1alpha1 contains API Schema definitions for the propeller v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=propeller.absmach.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion = schema.GroupVersion{Group: "propeller.absmach.io", Version: "v1alpha1"}

	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
