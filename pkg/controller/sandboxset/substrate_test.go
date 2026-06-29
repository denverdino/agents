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

package sandboxset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/openkruise/agents/api/v1alpha1"
)

func TestBuildActorTemplateFromSandboxSet(t *testing.T) {
	tests := []struct {
		name             string
		sbs              *agentsv1alpha1.SandboxSet
		expectContainers int
		expectPauseImage string
		expectError      string
	}{
		{
			name: "basic SandboxSet with inline template",
			sbs: &agentsv1alpha1.SandboxSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "code-interpreter",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationBackend:                    BackendSubstrate,
						AnnotationSubstrateSnapshotsLocation: "gs://my-bucket/snapshots",
					},
				},
				Spec: agentsv1alpha1.SandboxSetSpec{
					Replicas: 5,
					EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
						Template: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "main", Image: "example/code-interpreter@sha256:abc123"},
								},
							},
						},
					},
				},
			},
			expectContainers: 0,
			expectPauseImage: "example/code-interpreter@sha256:abc123",
		},
		{
			name: "SandboxSet with multiple containers",
			sbs: &agentsv1alpha1.SandboxSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-container",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationBackend: BackendSubstrate,
					},
				},
				Spec: agentsv1alpha1.SandboxSetSpec{
					Replicas: 3,
					EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
						Template: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "main", Image: "main-image:v1"},
									{Name: "sidecar", Image: "sidecar-image:v1", Command: []string{"/bin/sidecar"}},
								},
							},
						},
					},
				},
			},
			expectContainers: 1,
			expectPauseImage: "main-image:v1",
		},
		{
			name: "SandboxSet with no template",
			sbs: &agentsv1alpha1.SandboxSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-template",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationBackend: BackendSubstrate,
					},
				},
				Spec: agentsv1alpha1.SandboxSetSpec{
					Replicas: 1,
				},
			},
			expectContainers: 0,
			expectPauseImage: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at, err := buildActorTemplateSpec(tt.sbs)
			if tt.expectError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectPauseImage, at.PauseImage)
			assert.Len(t, at.Containers, tt.expectContainers)
		})
	}
}

func TestBuildWorkerPoolFromSandboxSet(t *testing.T) {
	tests := []struct {
		name           string
		sbs            *agentsv1alpha1.SandboxSet
		expectReplicas int32
		expectError    string
	}{
		{
			name: "WorkerPool replicas match SandboxSet.spec.replicas",
			sbs: &agentsv1alpha1.SandboxSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "code-interpreter",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationBackend:             BackendSubstrate,
						AnnotationSubstrateAteomImage: "substrate/ateom-gvisor:latest",
					},
				},
				Spec: agentsv1alpha1.SandboxSetSpec{
					Replicas: 10,
				},
			},
			expectReplicas: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wp, err := buildWorkerPoolSpec(tt.sbs)
			if tt.expectError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectReplicas, wp.Replicas)
		})
	}
}

func TestIsSubstrateBackend(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expect      bool
	}{
		{
			name:        "substrate backend",
			annotations: map[string]string{AnnotationBackend: BackendSubstrate},
			expect:      true,
		},
		{
			name:        "no backend annotation",
			annotations: map[string]string{},
			expect:      false,
		},
		{
			name:        "other backend",
			annotations: map[string]string{AnnotationBackend: "other"},
			expect:      false,
		},
		{
			name:        "nil annotations",
			annotations: nil,
			expect:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, IsSubstrateBackend(tt.annotations))
		})
	}
}
