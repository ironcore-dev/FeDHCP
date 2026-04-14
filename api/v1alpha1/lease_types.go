// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="MAC",type=string,JSONPath=`.spec.mac`
// +kubebuilder:printcolumn:name="IP",type=string,JSONPath=`.spec.ip`,priority=1
// +kubebuilder:selectablefield:JSONPath=`.spec.mac`
// +kubebuilder:selectablefield:JSONPath=`.spec.ip`

// Lease records a DHCP lease issued by fedhcp.
type Lease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec LeaseSpec `json:"spec,omitempty"`
}

type LeaseSpec struct {
	MAC       string      `json:"mac"`
	IP        string      `json:"ip"`
	FirstSeen metav1.Time `json:"firstSeen"`
	Renewed   metav1.Time `json:"renewed"`
	ExpiresAt metav1.Time `json:"expiresAt"`
}

// +kubebuilder:object:root=true

// LeaseList contains a list of Lease resources.
type LeaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Lease `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Lease{}, &LeaseList{})
}
