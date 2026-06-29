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

package substrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/openkruise/agents/api/v1alpha1"
	"github.com/openkruise/agents/pkg/controller/sandboxset"
	"github.com/openkruise/agents/pkg/sandbox-manager/infra"
)

func newTestInfra(sandboxSets []*agentsv1alpha1.SandboxSet) *SubstrateInfra {
	return &SubstrateInfra{
		metadata:  NewInMemoryMetadataStore(),
		locks:     NewKeyedLocker(),
		templates: NewTemplateResolver(sandboxSets),
	}
}

func TestSubstrateInfra_HasTemplate(t *testing.T) {
	tests := []struct {
		name        string
		sandboxSets []*agentsv1alpha1.SandboxSet
		opts        infra.HasTemplateOptions
		expect      bool
	}{
		{
			name: "returns true for substrate-backed SandboxSet with actor template",
			sandboxSets: []*agentsv1alpha1.SandboxSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "code-interpreter", Namespace: "default",
						Annotations: map[string]string{
							sandboxset.AnnotationBackend:      sandboxset.BackendSubstrate,
							AnnotationStatusActorTemplateName: "code-interpreter-abc123",
						},
					},
				},
			},
			opts:   infra.HasTemplateOptions{Namespace: "default", Name: "code-interpreter"},
			expect: true,
		},
		{
			name:        "returns false for non-existent SandboxSet",
			sandboxSets: []*agentsv1alpha1.SandboxSet{},
			opts:        infra.HasTemplateOptions{Namespace: "default", Name: "missing"},
			expect:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := newTestInfra(tt.sandboxSets)
			assert.Equal(t, tt.expect, i.HasTemplate(context.Background(), tt.opts))
		})
	}
}

func TestSubstrateInfra_GetSandbox(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(store SandboxMetadataStore)
		opts        infra.GetSandboxOptions
		expectID    string
		expectError string
	}{
		{
			name: "returns sandbox from metadata store",
			setup: func(store SandboxMetadataStore) {
				store.Put("sbx-1", &SandboxMetadata{
					SandboxID: "sbx-1",
					ActorID:   "actor-1",
					Namespace: "default",
					Owner:     "team-a",
					Phase:     PhaseRunning,
				})
			},
			opts:     infra.GetSandboxOptions{Namespace: "default", SandboxID: "sbx-1"},
			expectID: "sbx-1",
		},
		{
			name:        "returns error for non-existent sandbox",
			setup:       func(store SandboxMetadataStore) {},
			opts:        infra.GetSandboxOptions{Namespace: "default", SandboxID: "missing"},
			expectError: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := newTestInfra(nil)
			tt.setup(i.metadata)
			sbx, err := i.GetSandbox(context.Background(), tt.opts)
			if tt.expectError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectID, sbx.GetSandboxID())
		})
	}
}

func TestSubstrateInfra_UnsupportedOperations(t *testing.T) {
	i := newTestInfra(nil)
	ctx := context.Background()

	t.Run("CloneSandbox returns error", func(t *testing.T) {
		_, _, err := i.CloneSandbox(ctx, infra.CloneSandboxOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not supported")
	})

	t.Run("DeleteCheckpoint returns error", func(t *testing.T) {
		err := i.DeleteCheckpoint(ctx, infra.DeleteCheckpointOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not supported")
	})

	t.Run("HasCheckpoint returns false", func(t *testing.T) {
		assert.False(t, i.HasCheckpoint(ctx, infra.HasCheckpointOptions{}))
	})
}
