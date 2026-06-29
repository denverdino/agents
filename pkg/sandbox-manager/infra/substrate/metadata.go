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
	"fmt"
	"sync"
	"time"

	"github.com/openkruise/agents/pkg/utils/proxyutils"
)

const (
	PhaseRunning   = "running"
	PhasePaused    = "paused"
	PhaseSuspended = "suspended"
	PhaseResuming  = "resuming"
)

type SandboxMetadata struct {
	SandboxID         string
	ActorID           string
	Namespace         string
	SandboxSetName    string
	ActorTemplateName string
	Owner             string
	Route             proxyutils.Route
	Phase             string
	Timeout           time.Time
	CreateTime        time.Time
	LastActiveTime    time.Time
	HibernateMode     string
}

type MetadataListOptions struct {
	Owner     string
	Namespace string
}

type SandboxMetadataStore interface {
	Get(sandboxID string) (*SandboxMetadata, error)
	Put(sandboxID string, meta *SandboxMetadata)
	Delete(sandboxID string)
	List(opts MetadataListOptions) []*SandboxMetadata
	UpdatePhase(sandboxID string, phase string)
	UpdateRoute(sandboxID string, route proxyutils.Route)
	UpdateLastActive(sandboxID string, t time.Time)
}

type InMemoryMetadataStore struct {
	mu    sync.RWMutex
	store map[string]*SandboxMetadata
}

func NewInMemoryMetadataStore() *InMemoryMetadataStore {
	return &InMemoryMetadataStore{
		store: make(map[string]*SandboxMetadata),
	}
}

func (s *InMemoryMetadataStore) Get(sandboxID string) (*SandboxMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	meta, ok := s.store[sandboxID]
	if !ok {
		return nil, fmt.Errorf("sandbox metadata not found: %s", sandboxID)
	}
	cp := *meta
	return &cp, nil
}

func (s *InMemoryMetadataStore) Put(sandboxID string, meta *SandboxMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *meta
	s.store[sandboxID] = &cp
}

func (s *InMemoryMetadataStore) Delete(sandboxID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.store, sandboxID)
}

func (s *InMemoryMetadataStore) List(opts MetadataListOptions) []*SandboxMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*SandboxMetadata
	for _, meta := range s.store {
		if opts.Owner != "" && meta.Owner != opts.Owner {
			continue
		}
		if opts.Namespace != "" && meta.Namespace != opts.Namespace {
			continue
		}
		cp := *meta
		result = append(result, &cp)
	}
	return result
}

func (s *InMemoryMetadataStore) UpdatePhase(sandboxID string, phase string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if meta, ok := s.store[sandboxID]; ok {
		meta.Phase = phase
	}
}

func (s *InMemoryMetadataStore) UpdateRoute(sandboxID string, route proxyutils.Route) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if meta, ok := s.store[sandboxID]; ok {
		meta.Route = route
	}
}

func (s *InMemoryMetadataStore) UpdateLastActive(sandboxID string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if meta, ok := s.store[sandboxID]; ok {
		meta.LastActiveTime = t
	}
}
