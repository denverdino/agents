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
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	agentsv1alpha1 "github.com/openkruise/agents/api/v1alpha1"
)

const (
	// AnnotationBackend specifies the backend implementation for a SandboxSet.
	AnnotationBackend = "agents.kruise.io/backend"

	// BackendSubstrate is the annotation value indicating Substrate backend.
	BackendSubstrate = "substrate"

	// AnnotationSubstrateWorkerPoolName overrides the generated WorkerPool name.
	AnnotationSubstrateWorkerPoolName = "substrate.agents.kruise.io/worker-pool-name"

	// AnnotationSubstrateHibernateMode specifies the hibernate mode: "pause" or "suspend".
	AnnotationSubstrateHibernateMode = "substrate.agents.kruise.io/hibernate-mode"

	// AnnotationSubstrateAteomImage specifies the ateom container image for WorkerPool.
	AnnotationSubstrateAteomImage = "substrate.agents.kruise.io/ateom-image"

	// AnnotationSubstrateSnapshotsLocation specifies GCS location for ActorTemplate snapshots.
	AnnotationSubstrateSnapshotsLocation = "substrate.agents.kruise.io/snapshots-location"

	// AnnotationSubstrateSandboxClass specifies the sandbox class: "gvisor" or "microvm".
	AnnotationSubstrateSandboxClass = "substrate.agents.kruise.io/sandbox-class"

	// AnnotationStatusActorTemplateName records the current Substrate ActorTemplate name.
	AnnotationStatusActorTemplateName = "substrate.agents.kruise.io/actor-template-name"

	// AnnotationStatusWorkerPoolName records the current Substrate WorkerPool name.
	AnnotationStatusWorkerPoolName = "substrate.agents.kruise.io/worker-pool-name"

	HibernateModePause   = "pause"
	HibernateModeSuspend = "suspend"

	defaultAteomImage = "substrate/ateom-gvisor:latest"
)

// IsSubstrateBackend returns true if the SandboxSet is configured to use the Substrate backend.
func IsSubstrateBackend(annotations map[string]string) bool {
	return annotations[AnnotationBackend] == BackendSubstrate
}

// reconcileSubstrate handles SandboxSet reconciliation for substrate backend.
// It creates/updates ActorTemplate and WorkerPool CRDs instead of Sandbox CRs.
func (r *Reconciler) reconcileSubstrate(ctx context.Context, sbs *agentsv1alpha1.SandboxSet) (ctrl.Result, error) {
	log := klog.FromContext(ctx).WithValues("sandboxset", klog.KObj(sbs), "backend", "substrate")
	log.Info("reconciling substrate backend")

	atSpec, err := buildActorTemplateSpec(sbs)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build ActorTemplate spec: %w", err)
	}

	hash, err := computeSubstrateTemplateHash(atSpec)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("compute template hash: %w", err)
	}

	atName := fmt.Sprintf("%s-%s", sbs.Name, hash[:8])

	if err := r.ensureActorTemplate(ctx, sbs, atName, atSpec); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure ActorTemplate: %w", err)
	}

	wpSpec, err := buildWorkerPoolSpec(sbs)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build WorkerPool spec: %w", err)
	}

	wpName := sbs.Annotations[AnnotationSubstrateWorkerPoolName]
	if wpName == "" {
		wpName = sbs.Name
	}

	if err := r.ensureWorkerPool(ctx, sbs, wpName, wpSpec); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure WorkerPool: %w", err)
	}

	if err := r.updateSubstrateStatus(ctx, sbs, atName, wpName, hash); err != nil {
		return ctrl.Result{}, fmt.Errorf("update substrate status: %w", err)
	}

	log.Info("substrate reconcile complete",
		"actorTemplate", atName, "workerPool", wpName, "replicas", sbs.Spec.Replicas)
	return ctrl.Result{}, nil
}

func buildActorTemplateSpec(sbs *agentsv1alpha1.SandboxSet) (*atev1alpha1.ActorTemplateSpec, error) {
	spec := &atev1alpha1.ActorTemplateSpec{
		SnapshotsConfig: atev1alpha1.SnapshotsConfig{
			Location: sbs.Annotations[AnnotationSubstrateSnapshotsLocation],
		},
	}

	sandboxClass := sbs.Annotations[AnnotationSubstrateSandboxClass]
	if sandboxClass != "" {
		spec.SandboxClass = atev1alpha1.SandboxClass(sandboxClass)
	}

	if sbs.Spec.Template != nil && len(sbs.Spec.Template.Spec.Containers) > 0 {
		spec.PauseImage = sbs.Spec.Template.Spec.Containers[0].Image

		for _, c := range sbs.Spec.Template.Spec.Containers[1:] {
			sc := atev1alpha1.Container{
				Name:  c.Name,
				Image: c.Image,
			}
			for _, cmd := range c.Command {
				sc.Command = append(sc.Command, cmd)
			}
			for _, e := range c.Env {
				if e.Value != "" {
					v := e.Value
					sc.Env = append(sc.Env, atev1alpha1.EnvVar{
						Name:  e.Name,
						Value: &v,
					})
				}
			}
			spec.Containers = append(spec.Containers, sc)
		}
	}

	return spec, nil
}

func buildWorkerPoolSpec(sbs *agentsv1alpha1.SandboxSet) (*atev1alpha1.WorkerPoolSpec, error) {
	ateomImage := sbs.Annotations[AnnotationSubstrateAteomImage]
	if ateomImage == "" {
		ateomImage = defaultAteomImage
	}

	spec := &atev1alpha1.WorkerPoolSpec{
		Replicas:   sbs.Spec.Replicas,
		AteomImage: ateomImage,
	}

	sandboxClass := sbs.Annotations[AnnotationSubstrateSandboxClass]
	if sandboxClass != "" {
		spec.SandboxClass = atev1alpha1.SandboxClass(sandboxClass)
	}

	return spec, nil
}

func (r *Reconciler) ensureActorTemplate(ctx context.Context, sbs *agentsv1alpha1.SandboxSet, name string, spec *atev1alpha1.ActorTemplateSpec) error {
	at := &atev1alpha1.ActorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sbs.Namespace,
			Labels: map[string]string{
				"agents.kruise.io/sandboxset": sbs.Name,
			},
		},
		Spec: *spec,
	}

	if err := ctrl.SetControllerReference(sbs, at, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference: %w", err)
	}

	if err := r.Create(ctx, at); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	return nil
}

func (r *Reconciler) ensureWorkerPool(ctx context.Context, sbs *agentsv1alpha1.SandboxSet, name string, spec *atev1alpha1.WorkerPoolSpec) error {
	existing := &atev1alpha1.WorkerPool{}
	err := r.Get(ctx, client.ObjectKey{Namespace: sbs.Namespace, Name: name}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		wp := &atev1alpha1.WorkerPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: sbs.Namespace,
				Labels: map[string]string{
					"agents.kruise.io/sandboxset": sbs.Name,
				},
			},
			Spec: *spec,
		}
		if err := ctrl.SetControllerReference(sbs, wp, r.Scheme); err != nil {
			return fmt.Errorf("set owner reference: %w", err)
		}
		return r.Create(ctx, wp)
	}

	if existing.Spec.Replicas != spec.Replicas {
		existing.Spec.Replicas = spec.Replicas
		return r.Update(ctx, existing)
	}
	return nil
}

func (r *Reconciler) updateSubstrateStatus(ctx context.Context, sbs *agentsv1alpha1.SandboxSet, atName, wpName, hash string) error {
	annotations := sbs.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}
	needUpdate := false
	if annotations[AnnotationStatusActorTemplateName] != atName {
		annotations[AnnotationStatusActorTemplateName] = atName
		needUpdate = true
	}
	if annotations[AnnotationStatusWorkerPoolName] != wpName {
		annotations[AnnotationStatusWorkerPoolName] = wpName
		needUpdate = true
	}

	if needUpdate {
		sbs.Annotations = annotations
		if err := r.Update(ctx, sbs); err != nil {
			return err
		}
	}

	newStatus := sbs.Status.DeepCopy()
	newStatus.UpdateRevision = hash
	newStatus.Replicas = sbs.Spec.Replicas
	return r.updateSandboxSetStatus(ctx, *newStatus, sbs)
}

func computeSubstrateTemplateHash(spec *atev1alpha1.ActorTemplateSpec) (string, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:4]), nil
}
