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
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/natali-lior/kube-hpc-sentinel/api/v1alpha1"
	"github.com/natali-lior/kube-hpc-sentinel/pkg/config"
	kube "github.com/natali-lior/kube-hpc-sentinel/pkg/kube"
	"github.com/natali-lior/kube-hpc-sentinel/pkg/prometheus"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HPCJobReconciler reconciles a HPCJob object
type HPCJobReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Cfg           *config.Config
	KubeClient    *kube.KubeClient
	PromAPIClient *prometheus.MetricsProvider
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
		For(&v1alpha1.HPCJob{}).
		Named("hpcjob").
		Complete(r)
}

func (r *HPCJobReconciler) fetchCRDResource(ctx context.Context, req ctrl.Request) (v1alpha1.HPCJob, error) {
	l := log.Ctx(ctx)
	var hpcJob v1alpha1.HPCJob
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
	podExists, result, err := r.manageExistingPod(ctx, job)
	if err != nil {
		return result, err
	}
	if podExists {
		return result, nil
	}
	valid := r.validateJobSpec(ctx, job)
	if !valid {
		log.Ctx(ctx).Error().Msgf("hpc job spec is invalid [%s]", job.Name)
		if err := r.updateJobStatus(ctx, job, v1alpha1.Failed); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	healthyNodes, err := r.fetchNodesAndSyncStatus(ctx, job)
	if err != nil {
		return ctrl.Result{}, err
	}
	requiredCapacity := int64(job.Spec.GPUCount)
	allocated, hasCapacity := r.calculateClusterCapacityForJob(ctx, healthyNodes, requiredCapacity)
	if !hasCapacity {
		log.Ctx(ctx).Info().Msgf("not enough capacity in the cluster for hpc job [%s], rechecking in 5 minutes", job.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}
	if err := r.createHpcPod(ctx, job, allocated); err != nil {
		log.Ctx(ctx).Error().Msgf("failed to create hpc pod for job [%s]: %v", job.Name, err)
		return ctrl.Result{}, err
	}
	log.Ctx(ctx).Info().Msgf("successfully created hpc pod for job [%s]", job.Name)
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *HPCJobReconciler) handleRunning(ctx context.Context, job *v1alpha1.HPCJob) (ctrl.Result, error) {
	_, result, err := r.manageExistingPod(ctx, job)
	if err != nil {
		return result, err
	}
	return ctrl.Result{}, nil
}

func (r *HPCJobReconciler) handleFailed(_ context.Context, _ *v1alpha1.HPCJob) (ctrl.Result, error) {
	// todo: update counter metric for failed jobs
	return ctrl.Result{}, nil
}

func (r *HPCJobReconciler) manageExistingPod(ctx context.Context, job *v1alpha1.HPCJob) (bool, ctrl.Result, error) {
	podExists, err := r.scanPodStatus(ctx, job)
	if err != nil {
		return podExists, ctrl.Result{}, err
	}
	if podExists {
		if job.Status.Phase != v1alpha1.Running && job.Status.Phase != v1alpha1.Failed && job.Status.Phase != v1alpha1.Complete {
			return podExists, ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
		}
	}
	return podExists, ctrl.Result{}, nil
}

func (r *HPCJobReconciler) validateJobSpec(ctx context.Context, job *v1alpha1.HPCJob) bool {
	requiredCapacity := int64(job.Spec.GPUCount)
	return requiredCapacity > 0 && len(job.Spec.Image) > 0
}

func (r *HPCJobReconciler) scanPodStatus(ctx context.Context, job *v1alpha1.HPCJob) (bool, error) {
	pod, err := r.KubeClient.GetPod(ctx, job.Name, job.Namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil // pod does not exist
		} else {
			if err := r.updateJobStatus(ctx, job, v1alpha1.Failed); err != nil {
				return true, err // pod exists, update status failed
			}
			return true, err // pod exists, it is an error (other than not found)
		}
	} else {
		switch pod.Status.Phase {
		case corev1.PodRunning:
			if err := r.updateJobStatus(ctx, job, v1alpha1.Running); err != nil {
				return true, err // pod exists, failed to update status
			}
			return true, nil // pod exists, no error
		case corev1.PodFailed:
			if err := r.updateJobStatus(ctx, job, v1alpha1.Failed); err != nil {
				return true, err // pod exists, update status failed
			}
			return true, fmt.Errorf("pod %s is failing", pod.Name) // pod exists but failing
		case corev1.PodSucceeded:
			if err := r.updateJobStatus(ctx, job, v1alpha1.Complete); err != nil {
				return true, err // pod exists, update status failed
			}
			return true, nil // pod exists, no error
		}
		return true, nil // pod exists, no error
	}
}

func (r *HPCJobReconciler) fetchNodesAndSyncStatus(ctx context.Context, job *v1alpha1.HPCJob) ([]corev1.Node, error) {
	healthy, nonHealthy, err := ListGPUNodes(ctx, r.KubeClient, r.PromAPIClient)
	if err != nil {
		return nil, err
	}
	SyncNodeHealthStatus(ctx, r.KubeClient, healthy, nonHealthy)
	return healthy, nil
}

func (r *HPCJobReconciler) calculateClusterCapacityForJob(ctx context.Context, healthyNodes []corev1.Node, requiredCapacity int64) (int, bool) {
	candidates := []int{}
	for _, node := range healthyNodes {
		nodeAllocatable := r.KubeClient.GetNodeAllocatableGPUs(ctx, node)
		if node.Labels == nil {
			if nodeAllocatable >= requiredCapacity {
				candidates = append(candidates, 1)
			}
		} else {
			if val, ok := node.Labels[kube.HIGH_DENSITY_AFFINITY_LABEL]; ok {
				allocated, err := strconv.Atoi(val)
				if err != nil {
					log.Ctx(ctx).Error().Msgf("failed to parse high density affinity label on node [%s]: %v", node.Name, err)
				}
				if nodeAllocatable-int64(allocated) >= requiredCapacity {
					candidates = append(candidates, allocated)
				}
			}
		}
	}
	if len(candidates) > 0 {
		min := candidates[0]
		for _, val := range candidates {
			if val < min {
				min = val
			}
		}
		return min, true
	} else {
		return 0, false
	}
}

func (r *HPCJobReconciler) createHpcPod(ctx context.Context, hpcJob *v1alpha1.HPCJob, score int) error {
	if score > 0 {
		score -= 1
	}
	gpuQuantity := resource.MustParse(strconv.Itoa(int(hpcJob.Spec.GPUCount)))
	hpcPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpcJob.Name,
			Namespace: hpcJob.Namespace,
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
						{
							Weight: 80,
							Preference: corev1.NodeSelectorTerm{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      kube.HIGH_DENSITY_AFFINITY_LABEL,
										Operator: corev1.NodeSelectorOpGt,
										Values:   []string{fmt.Sprint(score)},
									},
								},
							},
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  hpcJob.Name,
					Image: hpcJob.Spec.Image,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							kube.GPU_CORES_CAPACITY: gpuQuantity,
						},
						Requests: corev1.ResourceList{
							kube.GPU_CORES_CAPACITY: gpuQuantity,
						},
					},
				},
			},
		},
	}
	return r.KubeClient.CreatePod(ctx, hpcPod)
}

func (r *HPCJobReconciler) updateJobStatus(ctx context.Context, job *v1alpha1.HPCJob, newPhase string) error {
	job.Status.Phase = newPhase
	if err := r.Status().Update(ctx, job); err != nil {
		log.Ctx(ctx).Error().Msgf("failed to update hpc job [%s] status: %v", job.Name, err)
		return err
	}
	return nil
}
