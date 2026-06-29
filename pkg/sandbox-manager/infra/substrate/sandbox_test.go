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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"

	"github.com/openkruise/agents/pkg/utils/proxyutils"
)

func TestSubstrateSandbox(t *testing.T) {
	tests := []struct {
		name   string
		meta   *SandboxMetadata
		expect func(t *testing.T, sbx *SubstrateSandbox)
	}{
		{
			name: "GetSandboxID returns sandbox ID",
			meta: &SandboxMetadata{SandboxID: "sbx-123", ActorID: "actor-456"},
			expect: func(t *testing.T, sbx *SubstrateSandbox) {
				assert.Equal(t, "sbx-123", sbx.GetSandboxID())
			},
		},
		{
			name: "GetRoute returns route from metadata",
			meta: &SandboxMetadata{
				SandboxID: "sbx-123",
				ActorID:   "actor-456",
				Owner:     "team-a",
				Route: proxyutils.Route{
					IP:    "10.0.0.1",
					ID:    "sbx-123",
					Owner: "team-a",
					State: "running",
				},
			},
			expect: func(t *testing.T, sbx *SubstrateSandbox) {
				route := sbx.GetRoute()
				assert.Equal(t, "10.0.0.1", route.IP)
				assert.Equal(t, "sbx-123", route.ID)
				assert.Equal(t, "team-a", route.Owner)
			},
		},
		{
			name: "GetState returns running state",
			meta: &SandboxMetadata{SandboxID: "sbx-123", Phase: PhaseRunning},
			expect: func(t *testing.T, sbx *SubstrateSandbox) {
				state, reason := sbx.GetState()
				assert.Equal(t, "running", state)
				assert.Empty(t, reason)
			},
		},
		{
			name: "GetState returns paused state",
			meta: &SandboxMetadata{SandboxID: "sbx-123", Phase: PhasePaused},
			expect: func(t *testing.T, sbx *SubstrateSandbox) {
				state, _ := sbx.GetState()
				assert.Equal(t, "paused", state)
			},
		},
		{
			name: "GetTemplate returns SandboxSet name",
			meta: &SandboxMetadata{SandboxID: "sbx-123", SandboxSetName: "code-interpreter"},
			expect: func(t *testing.T, sbx *SubstrateSandbox) {
				assert.Equal(t, "code-interpreter", sbx.GetTemplate())
			},
		},
		{
			name: "GetClaimTime returns create time",
			meta: &SandboxMetadata{SandboxID: "sbx-123", CreateTime: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)},
			expect: func(t *testing.T, sbx *SubstrateSandbox) {
				ct, err := sbx.GetClaimTime()
				assert.NoError(t, err)
				assert.Equal(t, time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC), ct)
			},
		},
		{
			name: "Phase returns metadata phase",
			meta: &SandboxMetadata{SandboxID: "sbx-123", Phase: PhaseRunning},
			expect: func(t *testing.T, sbx *SubstrateSandbox) {
				assert.Equal(t, PhaseRunning, sbx.Phase())
			},
		},
		{
			name: "metav1.Object methods work via embedded ObjectMeta",
			meta: &SandboxMetadata{SandboxID: "sbx-123", Namespace: "default", ActorID: "actor-456"},
			expect: func(t *testing.T, sbx *SubstrateSandbox) {
				assert.Equal(t, "default", sbx.GetNamespace())
				assert.Equal(t, "sbx-123", sbx.GetName())
				assert.Equal(t, types.UID("actor-456"), sbx.GetUID())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sbx := NewSubstrateSandbox(tt.meta, nil, nil, nil)
			tt.expect(t, sbx)
		})
	}
}
