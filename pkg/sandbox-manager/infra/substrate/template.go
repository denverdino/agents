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
	"github.com/openkruise/agents/pkg/controller/sandboxset"
)

const (
	AnnotationStatusActorTemplateName = "substrate.agents.kruise.io/actor-template-name"
	AnnotationStatusWorkerPoolName    = "substrate.agents.kruise.io/worker-pool-name"
)

type ResolvedTemplate struct {
	ActorTemplateName string
	Namespace         string
	SandboxSetName    string
	HibernateMode     string
}

type TemplateResolver struct {
	sandboxSets map[string]*agentsv1alpha1.SandboxSet
}

func NewTemplateResolver(sets []*agentsv1alpha1.SandboxSet) *TemplateResolver {
	m := make(map[string]*agentsv1alpha1.SandboxSet, len(sets))
	for _, s := range sets {
		m[s.Namespace+"/"+s.Name] = s
	}
	return &TemplateResolver{sandboxSets: m}
}

func (r *TemplateResolver) Resolve(_ context.Context, namespace, templateName string) (*ResolvedTemplate, error) {
	var sbs *agentsv1alpha1.SandboxSet
	if namespace != "" {
		sbs = r.sandboxSets[namespace+"/"+templateName]
	} else {
		for _, s := range r.sandboxSets {
			if s.Name == templateName {
				sbs = s
				break
			}
		}
	}
	if sbs == nil {
		return nil, fmt.Errorf("SandboxSet %s/%s not found", namespace, templateName)
	}

	if !sandboxset.IsSubstrateBackend(sbs.Annotations) {
		return nil, fmt.Errorf("SandboxSet %s/%s is not a substrate backend", namespace, templateName)
	}

	actorTemplateName := sbs.Annotations[AnnotationStatusActorTemplateName]
	if actorTemplateName == "" {
		return nil, fmt.Errorf("SandboxSet %s/%s has no actor template name annotation", namespace, templateName)
	}

	hibernateMode := sbs.Annotations[sandboxset.AnnotationSubstrateHibernateMode]
	if hibernateMode == "" {
		hibernateMode = sandboxset.HibernateModeSuspend
	}

	return &ResolvedTemplate{
		ActorTemplateName: actorTemplateName,
		Namespace:         sbs.Namespace,
		SandboxSetName:    sbs.Name,
		HibernateMode:     hibernateMode,
	}, nil
}
