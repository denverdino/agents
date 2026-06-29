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
	"io"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/openkruise/agents/pkg/controller/sandboxset"
	"github.com/openkruise/agents/pkg/sandbox-manager/infra"
	"github.com/openkruise/agents/pkg/utils/proxyutils"
	"github.com/openkruise/agents/pkg/utils/timeout"
)

type SubstrateSandbox struct {
	metav1.ObjectMeta

	meta     *SandboxMetadata
	client   ateapipb.ControlClient
	metadata SandboxMetadataStore
	locks    *KeyedLocker
	timeout  timeout.Options
	image    string
	labels   map[string]string
}

var _ infra.Sandbox = (*SubstrateSandbox)(nil)

func NewSubstrateSandbox(meta *SandboxMetadata, client ateapipb.ControlClient, metadataStore SandboxMetadataStore, locks *KeyedLocker) *SubstrateSandbox {
	sbx := &SubstrateSandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:              meta.SandboxID,
			Namespace:         meta.Namespace,
			UID:               types.UID(meta.ActorID),
			CreationTimestamp: metav1.NewTime(meta.CreateTime),
		},
		meta:     meta,
		client:   client,
		metadata: metadataStore,
		locks:    locks,
		labels:   make(map[string]string),
	}
	return sbx
}

func (s *SubstrateSandbox) Pause(ctx context.Context, opts infra.PauseOptions) error {
	log := klog.FromContext(ctx)
	if opts.Timeout != nil {
		s.timeout = *opts.Timeout
	}

	var err error
	switch s.meta.HibernateMode {
	case sandboxset.HibernateModePause:
		log.V(5).Info("pausing substrate actor", "actorID", s.meta.ActorID)
		_, err = s.client.PauseActor(ctx, &ateapipb.PauseActorRequest{ActorId: s.meta.ActorID})
	default:
		log.V(5).Info("suspending substrate actor", "actorID", s.meta.ActorID)
		_, err = s.client.SuspendActor(ctx, &ateapipb.SuspendActorRequest{ActorId: s.meta.ActorID})
	}
	if err != nil {
		return fmt.Errorf("pause actor %s: %w", s.meta.ActorID, err)
	}

	if s.metadata != nil {
		s.metadata.UpdatePhase(s.meta.SandboxID, PhasePaused)
	}
	return nil
}

func (s *SubstrateSandbox) Resume(ctx context.Context, _ infra.ResumeOptions) error {
	log := klog.FromContext(ctx)
	log.V(5).Info("resuming substrate actor", "actorID", s.meta.ActorID)

	resp, err := s.client.ResumeActor(ctx, &ateapipb.ResumeActorRequest{
		ActorId: s.meta.ActorID,
		Boot:    false,
	})
	if err != nil {
		return fmt.Errorf("resume actor %s: %w", s.meta.ActorID, err)
	}

	if s.metadata != nil {
		s.metadata.UpdatePhase(s.meta.SandboxID, PhaseRunning)
		if resp.Actor != nil && resp.Actor.AteomPodIp != "" {
			route := buildRouteFromActor(s.meta.SandboxID, s.meta.Owner, resp.Actor)
			s.metadata.UpdateRoute(s.meta.SandboxID, route)
			s.meta.Route = route
		}
	}
	return nil
}

func (s *SubstrateSandbox) Kill(ctx context.Context) error {
	log := klog.FromContext(ctx)
	log.V(5).Info("killing substrate actor", "actorID", s.meta.ActorID)

	if s.meta.Phase == PhaseRunning || s.meta.Phase == PhaseResuming {
		if _, err := s.client.SuspendActor(ctx, &ateapipb.SuspendActorRequest{ActorId: s.meta.ActorID}); err != nil {
			log.V(5).Info("suspend before delete failed, attempting delete anyway", "err", err)
		}
	}

	if _, err := s.client.DeleteActor(ctx, &ateapipb.DeleteActorRequest{ActorId: s.meta.ActorID}); err != nil {
		return fmt.Errorf("delete actor %s: %w", s.meta.ActorID, err)
	}

	if s.metadata != nil {
		s.metadata.Delete(s.meta.SandboxID)
	}
	if s.locks != nil {
		s.locks.Delete(s.meta.ActorID)
	}
	return nil
}

func (s *SubstrateSandbox) GetSandboxID() string {
	return s.meta.SandboxID
}

func (s *SubstrateSandbox) GetRoute() proxyutils.Route {
	return s.meta.Route
}

func (s *SubstrateSandbox) GetState() (string, string) {
	return s.meta.Phase, ""
}

func (s *SubstrateSandbox) GetTemplate() string {
	return s.meta.SandboxSetName
}

func (s *SubstrateSandbox) GetResource() infra.SandboxResource {
	return infra.SandboxResource{}
}

func (s *SubstrateSandbox) SetImage(image string)            { s.image = image }
func (s *SubstrateSandbox) GetImage() string                 { return s.image }
func (s *SubstrateSandbox) SetPodLabels(l map[string]string) { s.labels = l }
func (s *SubstrateSandbox) GetPodLabels() map[string]string  { return s.labels }

func (s *SubstrateSandbox) SetTimeout(opts timeout.Options) { s.timeout = opts }

func (s *SubstrateSandbox) SaveTimeoutWithPolicy(_ context.Context, opts timeout.Options, _ timeout.UpdatePolicy) (infra.TimeoutUpdateResult, error) {
	s.timeout = opts
	if s.metadata != nil {
		s.metadata.UpdateLastActive(s.meta.SandboxID, time.Now())
	}
	return infra.TimeoutUpdateResult{Updated: true}, nil
}

func (s *SubstrateSandbox) GetTimeout() timeout.Options { return s.timeout }

func (s *SubstrateSandbox) GetClaimTime() (time.Time, error) {
	return s.meta.CreateTime, nil
}

func (s *SubstrateSandbox) TriggerReuse(_ context.Context) error {
	return fmt.Errorf("reuse not supported for substrate sandboxes")
}

func (s *SubstrateSandbox) IsReuseEnabled() bool { return false }

func (s *SubstrateSandbox) Phase() string { return s.meta.Phase }

func (s *SubstrateSandbox) InplaceRefresh(ctx context.Context, _ bool) error {
	if s.client == nil {
		return nil
	}
	resp, err := s.client.GetActor(ctx, &ateapipb.GetActorRequest{ActorId: s.meta.ActorID})
	if err != nil {
		return fmt.Errorf("refresh actor %s: %w", s.meta.ActorID, err)
	}
	if resp.Actor != nil {
		s.meta.Phase = actorStatusToPhase(resp.Actor.Status)
		if resp.Actor.AteomPodIp != "" {
			s.meta.Route = buildRouteFromActor(s.meta.SandboxID, s.meta.Owner, resp.Actor)
		}
	}
	return nil
}

func (s *SubstrateSandbox) Request(_ context.Context, _, _ string, _ int, _ io.Reader) (*http.Response, error) {
	return nil, fmt.Errorf("direct request not supported for substrate sandboxes")
}

func (s *SubstrateSandbox) CSIMount(_ context.Context, _, _ string) error {
	return fmt.Errorf("CSI mount not supported for substrate sandboxes")
}

func (s *SubstrateSandbox) CreateCheckpoint(_ context.Context, _ infra.CreateCheckpointOptions) (string, error) {
	return "", fmt.Errorf("checkpoint not supported for substrate sandboxes in this version")
}

func buildRouteFromActor(sandboxID, owner string, actor *ateapipb.Actor) proxyutils.Route {
	state := actorStatusToPhase(actor.Status)
	return proxyutils.Route{
		IP:    actor.AteomPodIp,
		ID:    sandboxID,
		UID:   types.UID(actor.ActorId),
		Owner: owner,
		State: state,
	}
}

func actorStatusToPhase(status ateapipb.Actor_Status) string {
	switch status {
	case ateapipb.Actor_STATUS_RUNNING:
		return PhaseRunning
	case ateapipb.Actor_STATUS_RESUMING:
		return PhaseResuming
	case ateapipb.Actor_STATUS_PAUSED, ateapipb.Actor_STATUS_PAUSING:
		return PhasePaused
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_SUSPENDING:
		return PhaseSuspended
	default:
		return ""
	}
}
