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

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/natali-lior/kube-hpc-sentinel/api/v1alpha1"
	hpcv1alpha1 "github.com/natali-lior/kube-hpc-sentinel/api/v1alpha1"
	"github.com/rs/zerolog/log"
)

// HPCJobReconciler reconciles a HPCJob object
type HPCJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hpc.nvidia.com,resources=hpcjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hpc.nvidia.com,resources=hpcjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hpc.nvidia.com,resources=hpcjobs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *HPCJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.With().Str("job_name", req.Name).Str("namespace", req.Namespace).Logger()
	ctx = l.WithContext(ctx)
	l.Info().Msg("Starting reconciliation")

	hpcJob, err := r.fetchCRDResource(ctx, req)
	if err != nil {
		l.Error().Msg("Failed to fetch HPCJob")
		return ctrl.Result{}, nil
	}
	if len(hpcJob.Status.Phase) == 0 {
		hpcJob.Status.Phase = v1alpha1.Pending
	}
	l.Info().Str("image", hpcJob.Spec.Image).Int32("gpus", hpcJob.Spec.GPUCount).Msg("Successfully loaded HPCJob spec")
	switch hpcJob.Status.Phase {
	case v1alpha1.Pending:
		return r.handlePending(ctx, &hpcJob)
	case v1alpha1.Running:
		return r.handleRunning(ctx, &hpcJob)
	case v1alpha1.Failed:
		return r.handleFailed(ctx, &hpcJob)
	}
	return ctrl.Result{}, nil
}

func (r *HPCJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hpcv1alpha1.HPCJob{}).
		Named("hpcjob").
		Complete(r)
}

func (r *HPCJobReconciler) fetchCRDResource(ctx context.Context, req ctrl.Request) (hpcv1alpha1.HPCJob, error) {
	l := log.Ctx(ctx)
	var hpcJob hpcv1alpha1.HPCJob
	if err := r.Get(ctx, req.NamespacedName, &hpcJob); err != nil {
		if errors.IsNotFound(err) {
			l.Info().Msg("HPCJob resource not found. Skipping...")
			return hpcJob, err
		}
		return hpcJob, err
	}
	return hpcJob, nil
}

func (r *HPCJobReconciler) handlePending(ctx context.Context, job *v1alpha1.HPCJob) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *HPCJobReconciler) handleRunning(ctx context.Context, job *v1alpha1.HPCJob) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *HPCJobReconciler) handleFailed(ctx context.Context, job *v1alpha1.HPCJob) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}
