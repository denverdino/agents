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
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/openkruise/agents/pkg/cache"
	managererrors "github.com/openkruise/agents/pkg/sandbox-manager/errors"
	"github.com/openkruise/agents/pkg/sandbox-manager/infra"
	"github.com/openkruise/agents/pkg/utils/proxyutils"
)

type SubstrateInfra struct {
	client    *SubstrateClient
	metadata  SandboxMetadataStore
	locks     *KeyedLocker
	templates *TemplateResolver
	cache     cache.Provider

	cleanupStopCh chan struct{}
}

var _ infra.Infrastructure = (*SubstrateInfra)(nil)

func (i *SubstrateInfra) Run(ctx context.Context) error {
	i.cleanupStopCh = make(chan struct{})
	if i.cache != nil {
		return i.cache.Run(ctx)
	}
	return nil
}

func (i *SubstrateInfra) Stop(_ context.Context) {
	if i.cleanupStopCh != nil {
		close(i.cleanupStopCh)
	}
	if i.client != nil {
		i.client.Close()
	}
	if i.cache != nil {
		i.cache.Stop(context.Background())
	}
}

func (i *SubstrateInfra) HasTemplate(ctx context.Context, opts infra.HasTemplateOptions) bool {
	_, err := i.templates.Resolve(ctx, opts.Namespace, opts.Name)
	return err == nil
}

func (i *SubstrateInfra) HasCheckpoint(_ context.Context, _ infra.HasCheckpointOptions) bool {
	return false
}

func (i *SubstrateInfra) GetCache() cache.Provider {
	return i.cache
}

func (i *SubstrateInfra) LoadDebugInfo() map[string]any {
	metas := i.metadata.List(MetadataListOptions{})
	return map[string]any{
		"backend":       "substrate",
		"sandbox_count": len(metas),
	}
}

func (i *SubstrateInfra) SelectSandboxes(_ context.Context, opts infra.SelectSandboxesOptions) ([]infra.Sandbox, error) {
	metas := i.metadata.List(MetadataListOptions{
		Owner:     opts.User,
		Namespace: opts.Namespace,
	})
	result := make([]infra.Sandbox, 0, len(metas))
	for _, m := range metas {
		var controlClient ateapipb.ControlClient
		if i.client != nil {
			controlClient = i.client.Control()
		}
		result = append(result, NewSubstrateSandbox(m, controlClient, i.metadata, i.locks))
	}
	return result, nil
}

func (i *SubstrateInfra) GetSandbox(_ context.Context, opts infra.GetSandboxOptions) (infra.Sandbox, error) {
	meta, err := i.metadata.Get(opts.SandboxID)
	if err != nil {
		return nil, managererrors.NewError(managererrors.ErrorNotFound, "sandbox %s not found", opts.SandboxID)
	}
	var controlClient ateapipb.ControlClient
	if i.client != nil {
		controlClient = i.client.Control()
	}
	return NewSubstrateSandbox(meta, controlClient, i.metadata, i.locks), nil
}

func (i *SubstrateInfra) SelectSucceededCheckpoints(_ context.Context, _ infra.SelectSucceededCheckpointsOptions) ([]infra.CheckpointInfo, error) {
	return nil, nil
}

func (i *SubstrateInfra) ClaimSandbox(ctx context.Context, opts infra.ClaimSandboxOptions) (infra.Sandbox, infra.ClaimMetrics, error) {
	log := klog.FromContext(ctx)
	metrics := infra.ClaimMetrics{}
	start := time.Now()
	defer func() { metrics.Total = time.Since(start) }()

	resolved, err := i.templates.Resolve(ctx, opts.Namespace, opts.Template)
	if err != nil {
		return nil, metrics, managererrors.NewError(managererrors.ErrorNotFound,
			"template %s/%s: %v", opts.Namespace, opts.Template, err)
	}

	ns := opts.Namespace
	if ns == "" {
		ns = resolved.Namespace
	}
	sandboxID := fmt.Sprintf("%s--%s", ns, uuid.New().String()[:8])
	actorID := uuid.New().String()

	lock := i.locks.Get(actorID)
	lock.Lock()
	defer lock.Unlock()

	log.Info("creating substrate actor",
		"sandboxID", sandboxID, "actorID", actorID,
		"actorTemplate", resolved.ActorTemplateName,
		"namespace", resolved.Namespace)

	resp, err := i.client.Control().CreateActor(ctx, &ateapipb.CreateActorRequest{
		ActorId:                actorID,
		ActorTemplateNamespace: resolved.Namespace,
		ActorTemplateName:      resolved.ActorTemplateName,
	})
	if err != nil {
		return nil, metrics, fmt.Errorf("create actor: %w", err)
	}

	resumeResp, err := i.client.Control().ResumeActor(ctx, &ateapipb.ResumeActorRequest{
		ActorId: actorID,
		Boot:    false,
	})
	if err != nil {
		return nil, metrics, fmt.Errorf("resume newly created actor %s: %w", actorID, err)
	}

	actor := resumeResp.Actor
	if actor == nil {
		actor = resp.Actor
	}

	route := proxyutils.Route{
		ID:    sandboxID,
		Owner: opts.User,
		State: "running",
	}
	if actor != nil && actor.AteomPodIp != "" {
		route.IP = actor.AteomPodIp
		route.UID = types.UID(actor.ActorId)
	}

	now := time.Now()
	meta := &SandboxMetadata{
		SandboxID:         sandboxID,
		ActorID:           actorID,
		Namespace:         opts.Namespace,
		SandboxSetName:    resolved.SandboxSetName,
		ActorTemplateName: resolved.ActorTemplateName,
		Owner:             opts.User,
		Route:             route,
		Phase:             PhaseRunning,
		CreateTime:        now,
		LastActiveTime:    now,
		HibernateMode:     resolved.HibernateMode,
	}
	i.metadata.Put(sandboxID, meta)

	sbx := NewSubstrateSandbox(meta, i.client.Control(), i.metadata, i.locks)

	if opts.Modifier != nil {
		opts.Modifier(sbx)
	}

	metrics.LockType = infra.LockTypeCreate
	return sbx, metrics, nil
}

func (i *SubstrateInfra) CloneSandbox(_ context.Context, _ infra.CloneSandboxOptions) (infra.Sandbox, infra.CloneMetrics, error) {
	return nil, infra.CloneMetrics{}, fmt.Errorf("clone not supported for substrate backend")
}

func (i *SubstrateInfra) DeleteCheckpoint(_ context.Context, _ infra.DeleteCheckpointOptions) error {
	return fmt.Errorf("checkpoint not supported for substrate backend")
}
