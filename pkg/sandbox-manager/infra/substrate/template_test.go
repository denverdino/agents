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
)

func TestTemplateResolver(t *testing.T) {
	tests := []struct {
		name            string
		sandboxSets     []*agentsv1alpha1.SandboxSet
		templateName    string
		namespace       string
		expectTemplate  string
		expectNamespace string
		expectError     string
	}{
		{
			name: "resolve template from substrate-backed SandboxSet",
			sandboxSets: []*agentsv1alpha1.SandboxSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "code-interpreter",
						Namespace: "default",
						Annotations: map[string]string{
							sandboxset.AnnotationBackend:      sandboxset.BackendSubstrate,
							AnnotationStatusActorTemplateName: "code-interpreter-abc123",
						},
					},
				},
			},
			templateName:    "code-interpreter",
			namespace:       "default",
			expectTemplate:  "code-interpreter-abc123",
			expectNamespace: "default",
		},
		{
			name: "non-substrate SandboxSet is skipped",
			sandboxSets: []*agentsv1alpha1.SandboxSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "code-interpreter",
						Namespace: "default",
					},
				},
			},
			templateName: "code-interpreter",
			namespace:    "default",
			expectError:  "not a substrate backend",
		},
		{
			name:         "SandboxSet not found",
			sandboxSets:  []*agentsv1alpha1.SandboxSet{},
			templateName: "does-not-exist",
			namespace:    "default",
			expectError:  "not found",
		},
		{
			name: "missing actor template annotation",
			sandboxSets: []*agentsv1alpha1.SandboxSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "code-interpreter",
						Namespace: "default",
						Annotations: map[string]string{
							sandboxset.AnnotationBackend: sandboxset.BackendSubstrate,
						},
					},
				},
			},
			templateName: "code-interpreter",
			namespace:    "default",
			expectError:  "has no actor template name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewTemplateResolver(tt.sandboxSets)
			result, err := resolver.Resolve(context.Background(), tt.namespace, tt.templateName)
			if tt.expectError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectTemplate, result.ActorTemplateName)
			assert.Equal(t, tt.expectNamespace, result.Namespace)
		})
	}
}
