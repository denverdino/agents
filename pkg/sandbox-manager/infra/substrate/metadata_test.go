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
	"github.com/stretchr/testify/require"

	"github.com/openkruise/agents/pkg/utils/proxyutils"
)

func TestInMemoryMetadataStore(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		setup       func(store *InMemoryMetadataStore)
		action      func(store *InMemoryMetadataStore) (interface{}, error)
		verify      func(t *testing.T, result interface{})
		expectError string
	}{
		{
			name: "Put and Get sandbox metadata",
			setup: func(store *InMemoryMetadataStore) {
				store.Put("sbx-1", &SandboxMetadata{
					SandboxID:         "sbx-1",
					ActorID:           "actor-1",
					Namespace:         "default",
					SandboxSetName:    "set-1",
					ActorTemplateName: "tmpl-1",
					Owner:             "team-a",
					Route: proxyutils.Route{
						IP:    "10.0.0.1",
						ID:    "route-1",
						Owner: "team-a",
						State: "active",
					},
					Phase:          PhaseRunning,
					Timeout:        now.Add(10 * time.Minute),
					CreateTime:     now,
					LastActiveTime: now,
					HibernateMode:  "none",
				})
			},
			action: func(store *InMemoryMetadataStore) (interface{}, error) {
				return store.Get("sbx-1")
			},
			verify: func(t *testing.T, result interface{}) {
				meta := result.(*SandboxMetadata)
				assert.Equal(t, "sbx-1", meta.SandboxID)
				assert.Equal(t, "actor-1", meta.ActorID)
				assert.Equal(t, "default", meta.Namespace)
				assert.Equal(t, "set-1", meta.SandboxSetName)
				assert.Equal(t, "tmpl-1", meta.ActorTemplateName)
				assert.Equal(t, "team-a", meta.Owner)
				assert.Equal(t, "10.0.0.1", meta.Route.IP)
				assert.Equal(t, "route-1", meta.Route.ID)
				assert.Equal(t, "team-a", meta.Route.Owner)
				assert.Equal(t, "active", meta.Route.State)
				assert.Equal(t, PhaseRunning, meta.Phase)
				assert.Equal(t, "none", meta.HibernateMode)
				assert.WithinDuration(t, now, meta.CreateTime, time.Millisecond)
				assert.WithinDuration(t, now, meta.LastActiveTime, time.Millisecond)
				assert.WithinDuration(t, now.Add(10*time.Minute), meta.Timeout, time.Millisecond)
			},
			expectError: "",
		},
		{
			name:  "Get non-existent sandbox returns error",
			setup: func(store *InMemoryMetadataStore) {},
			action: func(store *InMemoryMetadataStore) (interface{}, error) {
				return store.Get("non-existent")
			},
			verify:      nil,
			expectError: "sandbox metadata not found",
		},
		{
			name: "Delete sandbox metadata",
			setup: func(store *InMemoryMetadataStore) {
				store.Put("sbx-del", &SandboxMetadata{
					SandboxID: "sbx-del",
					Owner:     "team-a",
				})
			},
			action: func(store *InMemoryMetadataStore) (interface{}, error) {
				store.Delete("sbx-del")
				return store.Get("sbx-del")
			},
			verify:      nil,
			expectError: "sandbox metadata not found",
		},
		{
			name: "List by owner and namespace",
			setup: func(store *InMemoryMetadataStore) {
				store.Put("sbx-a1", &SandboxMetadata{
					SandboxID: "sbx-a1",
					Owner:     "team-a",
					Namespace: "ns-a",
				})
				store.Put("sbx-a2", &SandboxMetadata{
					SandboxID: "sbx-a2",
					Owner:     "team-a",
					Namespace: "ns-a",
				})
				store.Put("sbx-b1", &SandboxMetadata{
					SandboxID: "sbx-b1",
					Owner:     "team-b",
					Namespace: "ns-b",
				})
			},
			action: func(store *InMemoryMetadataStore) (interface{}, error) {
				result := store.List(MetadataListOptions{
					Owner:     "team-a",
					Namespace: "ns-a",
				})
				return result, nil
			},
			verify: func(t *testing.T, result interface{}) {
				items := result.([]*SandboxMetadata)
				assert.Len(t, items, 2)
				ids := map[string]bool{}
				for _, m := range items {
					ids[m.SandboxID] = true
					assert.Equal(t, "team-a", m.Owner)
					assert.Equal(t, "ns-a", m.Namespace)
				}
				assert.True(t, ids["sbx-a1"])
				assert.True(t, ids["sbx-a2"])
			},
			expectError: "",
		},
		{
			name: "UpdatePhase changes the phase",
			setup: func(store *InMemoryMetadataStore) {
				store.Put("sbx-phase", &SandboxMetadata{
					SandboxID: "sbx-phase",
					Phase:     PhaseRunning,
				})
			},
			action: func(store *InMemoryMetadataStore) (interface{}, error) {
				store.UpdatePhase("sbx-phase", PhasePaused)
				return store.Get("sbx-phase")
			},
			verify: func(t *testing.T, result interface{}) {
				meta := result.(*SandboxMetadata)
				assert.Equal(t, PhasePaused, meta.Phase)
			},
			expectError: "",
		},
		{
			name: "Copy-on-read prevents mutation of stored metadata",
			setup: func(store *InMemoryMetadataStore) {
				store.Put("sbx-copy", &SandboxMetadata{
					SandboxID: "sbx-copy",
					Owner:     "original-owner",
					Phase:     PhaseRunning,
				})
			},
			action: func(store *InMemoryMetadataStore) (interface{}, error) {
				// Get a copy and mutate it
				got, err := store.Get("sbx-copy")
				if err != nil {
					return nil, err
				}
				got.Owner = "mutated-owner"
				got.Phase = PhaseSuspended

				// Get again and verify original is unchanged
				return store.Get("sbx-copy")
			},
			verify: func(t *testing.T, result interface{}) {
				meta := result.(*SandboxMetadata)
				assert.Equal(t, "original-owner", meta.Owner)
				assert.Equal(t, PhaseRunning, meta.Phase)
			},
			expectError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryMetadataStore()
			tt.setup(store)

			result, err := tt.action(store)

			if tt.expectError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
			} else {
				require.NoError(t, err)
				if tt.verify != nil {
					tt.verify(t, result)
				}
			}
		})
	}
}
