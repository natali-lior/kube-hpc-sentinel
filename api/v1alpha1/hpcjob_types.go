/*
Copyright 2026.

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
)

var (
	NVLINK              = "nvlink"
	SupportedTopologies = []string{NVLINK}

	Pending         = "Pending"
	Running         = "Running"
	Complete        = "Complete"
	Failed          = "Failed"
	SupportedPhases = []string{Pending, Running, Complete, Failed}

	/* the resource is fully functional */
	Available = "Available"
	/* the resource is being created or updated */
	Progressing = "Progressing"
	/* the resource failed to reach or maintain its desired state */
	Degraded            = "Degraded"
	SupportedConditions = []string{Available, Progressing, Degraded}
)

var (
	DefaultTopology = ""
	DefaultPhase    = Pending
)

type SchedulingConstraints struct {
	Topology string `json:"topology,omitempty"`
}

type HPCJobSpec struct {
	// Image is the CUDA/MPI container to run.
	Image string `json:"image"`

	// GPUCount is the hardware floor. The operator will NOT schedule
	// the pod unless a single node has at least this many healthy GPUs.
	// +kubebuilder:validation:Minimum=1
	GPUCount int32 `json:"gpuCount"`

	// Constraints defines networking and hardware placement requirements.
	// +optional
	Constraints SchedulingConstraints `json:"constraints,omitempty"`
}

type HPCJobStatus struct {
	Phase string `json:"phase,omitempty"`
	// Conditions represent the latest observations of the job's state.
	// This is key for observability (Prometheus/Grafana integration).
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

type HPCJob struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of HPCJob
	// +required
	Spec HPCJobSpec `json:"spec"`

	// status defines the observed state of HPCJob
	// +optional
	Status HPCJobStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// HPCJobList contains a list of HPCJob
type HPCJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []HPCJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HPCJob{}, &HPCJobList{})
}
