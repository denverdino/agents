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
	"fmt"

	agentsv1alpha1 "github.com/openkruise/agents/api/v1alpha1"
	"github.com/openkruise/agents/pkg/cache"
	"github.com/openkruise/agents/pkg/sandbox-manager/infra"
)

type SubstrateInfraBuilder struct {
	instance *SubstrateInfra
}

var _ infra.Builder = (*SubstrateInfraBuilder)(nil)

func NewSubstrateInfraBuilder(substrateAddr string) *SubstrateInfraBuilder {
	return &SubstrateInfraBuilder{
		instance: &SubstrateInfra{
			metadata: NewInMemoryMetadataStore(),
			locks:    NewKeyedLocker(),
		},
	}
}

func (b *SubstrateInfraBuilder) WithSubstrateClient(client *SubstrateClient) *SubstrateInfraBuilder {
	b.instance.client = client
	return b
}

func (b *SubstrateInfraBuilder) WithCache(cache cache.Provider) *SubstrateInfraBuilder {
	b.instance.cache = cache
	return b
}

func (b *SubstrateInfraBuilder) WithSandboxSets(sets []*agentsv1alpha1.SandboxSet) *SubstrateInfraBuilder {
	b.instance.templates = NewTemplateResolver(sets)
	return b
}

func (b *SubstrateInfraBuilder) Build() infra.Infrastructure {
	return b.instance
}

func ConnectAndBuild(ctx context.Context, substrateAddr string, cacheProvider cache.Provider, sets []*agentsv1alpha1.SandboxSet) (infra.Infrastructure, error) {
	client, err := NewSubstrateClient(ctx, substrateAddr)
	if err != nil {
		return nil, fmt.Errorf("connect to substrate: %w", err)
	}

	return NewSubstrateInfraBuilder(substrateAddr).
		WithSubstrateClient(client).
		WithCache(cacheProvider).
		WithSandboxSets(sets).
		Build(), nil
}
