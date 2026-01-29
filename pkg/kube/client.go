package kube

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"encoding/json"

	"github.com/natali-lior/kube-hpc-sentinel/api/v1alpha1"
	"github.com/natali-lior/kube-hpc-sentinel/pkg/config"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	GPU_NODE_LABEL_INDICATOR = "nvidia.com/gpu.count"
	GPU_CORES_CAPACITY       = "nvidia.com/gpu"

	GPU_NODE_TAINT_KEY          = "nvidia.com/gpu"
	HIGH_DENSITY_AFFINITY_LABEL = "hpc-sentinel/density"
	UNHEALTHY_TAINT_KEY         = "hpc-sentinel/unhealthy"

	HPC_FINALIZER = "hpc-sentinel/finalizer"
)

type KubeClient struct {
	Kube   *kubernetes.Clientset
	Config *rest.Config
}

type GpuResourceStatus struct {
	Allocated   int64
	Allocatable int64
}

func NewKubeClient(cfg *config.Config) (*KubeClient, error) {
	config, err := GetClientConfig(cfg)
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return &KubeClient{
		Kube:   clientset,
		Config: config,
	}, nil
}

func GetClientConfig(cfg *config.Config) (*rest.Config, error) {
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}
	if cfg.KubeConfig == "" {
		if home := homedir.HomeDir(); home != "" {
			cfg.KubeConfig = filepath.Join(home, ".kube", "config")
		}
	}
	if _, err := os.Stat(cfg.KubeConfig); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubeconfig not found at %s", cfg.KubeConfig)
	}
	return clientcmd.BuildConfigFromFlags("", cfg.KubeConfig)
}

func NewClientFromRawConfig(raw []byte) (*KubeClient, error) {
	config, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &KubeClient{Kube: clientset, Config: config}, nil
}

func (k *KubeClient) GetClusterGPUNodes(ctx context.Context) ([]corev1.Node, error) {
	l := log.Ctx(ctx)
	gpuLabelExists, err := labels.NewRequirement(GPU_NODE_LABEL_INDICATOR, selection.Exists, nil)
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*gpuLabelExists)
	nodes, err := k.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}
	var validNodes []corev1.Node
	for _, node := range nodes.Items {
		countStr := node.Labels[GPU_NODE_LABEL_INDICATOR]
		count, err := strconv.Atoi(countStr)
		if err != nil || count <= 0 {
			l.Warn().Str("node", node.Name).Str("val", countStr).Msg("skipping node: invalid GPU count label")
			// export metric - counter for invalid gpu nodes / total count == 0
			continue
		}
		validNodes = append(validNodes, node)
	}
	l.Info().Int("count", len(validNodes)).Msg("successfully discovered GPU nodes")
	return validNodes, nil
}

func (k *KubeClient) GetNodeAllocatableGPUs(ctx context.Context, gpuNode corev1.Node) int64 {
	const gpuResourceName = corev1.ResourceName(GPU_CORES_CAPACITY)
	allocatableGpus := gpuNode.Status.Allocatable[gpuResourceName]
	return allocatableGpus.Value()
}

func (k *KubeClient) GetGpuNodeAllocations(ctx context.Context, gpuNode corev1.Node) (gpuStatus GpuResourceStatus, err error) {
	allocatableCount := k.GetNodeAllocatableGPUs(ctx, gpuNode)
	podList, err := k.Kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + gpuNode.Name,
	})
	if err != nil {
		return GpuResourceStatus{}, err
	}
	var allocatedCount int64
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			continue
		}
		for _, container := range pod.Spec.Containers {
			if req, ok := container.Resources.Requests[corev1.ResourceName(GPU_CORES_CAPACITY)]; ok {
				allocatedCount += req.Value()
			}
		}
	}

	return GpuResourceStatus{
		Allocated:   allocatedCount,
		Allocatable: allocatableCount,
	}, nil
}

func (k *KubeClient) LabelHighDensityNode(ctx context.Context, nodeName string, density int) error {
	patchData := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]string{
				HIGH_DENSITY_AFFINITY_LABEL: fmt.Sprint(density),
			},
		},
	}

	playloadBytes, err := json.Marshal(patchData)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	_, err = k.Kube.CoreV1().Nodes().Patch(
		ctx,
		nodeName,
		types.MergePatchType,
		playloadBytes,
		metav1.PatchOptions{},
	)

	if err != nil {
		return fmt.Errorf("failed to patch node %s: %w", nodeName, err)
	}

	return nil
}

func (k *KubeClient) CreatePod(ctx context.Context, pod *v1.Pod) error {
	_, err := k.Kube.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	return err
}

func (k *KubeClient) GetPod(ctx context.Context, name string, namespace string) (corev1.Pod, error) {
	pod, err := k.Kube.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	return *pod, err
}

func (k *KubeClient) IsolateUnhealthyNodes(ctx context.Context, unhealthyNodes []corev1.Node) error {
	l := log.Ctx(ctx)

	for _, node := range unhealthyNodes {
		unhealthyTaint := corev1.Taint{
			Key:    UNHEALTHY_TAINT_KEY,
			Value:  "true",
			Effect: corev1.TaintEffectNoSchedule,
		}

		patchData := map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels": map[string]string{
					HIGH_DENSITY_AFFINITY_LABEL: "0",
				},
			},
			"spec": map[string]interface{}{
				"taints": []corev1.Taint{unhealthyTaint},
			},
		}

		playloadBytes, err := json.Marshal(patchData)
		if err != nil {
			l.Error().Err(err).Str("node", node.Name).Msg("failed to marshal isolation patch")
			continue
		}

		_, err = k.Kube.CoreV1().Nodes().Patch(
			ctx,
			node.Name,
			types.StrategicMergePatchType,
			playloadBytes,
			metav1.PatchOptions{},
		)

		if err != nil {
			l.Error().Err(err).Str("node", node.Name).Msg("failed to patch unhealthy node")
			continue
		}

		l.Info().Str("node", node.Name).Msg("node isolated and density zeroed via patch")
	}
	return nil
}

// func (k *KubeClient) EvictHpcPod(ctx context.Context, podName string, namespace string) error {
// 	eviction := &policyv1.Eviction{
// 		ObjectMeta: v1.ObjectMeta{
// 			Name:      podName,
// 			Namespace: namespace,
// 		},
// 	}
// 	// This honors PodDisruptionBudgets and allows for a graceful shutdown period
// 	return k.Kube.PolicyV1().Evictions(namespace).Evict(ctx, eviction)
// }

func (k *KubeClient) RestoreHealthyNodes(ctx context.Context, healthyNodes []corev1.Node) error {
	l := log.Ctx(ctx)
	for _, node := range healthyNodes {
		var newTaints []corev1.Taint
		for _, t := range node.Spec.Taints {
			if t.Key != UNHEALTHY_TAINT_KEY {
				newTaints = append(newTaints, t)
			}
		}

		status, err := k.GetGpuNodeAllocations(ctx, node)
		if err != nil {
			l.Error().Err(err).Str("node", node.Name).Msg("failed to get allocations during restore")
			continue
		}
		available := status.Allocatable - status.Allocated

		patchData := map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels": map[string]string{
					HIGH_DENSITY_AFFINITY_LABEL: fmt.Sprint(available),
				},
			},
			"spec": map[string]interface{}{
				"taints": newTaints,
			},
		}

		playloadBytes, err := json.Marshal(patchData)
		if err != nil {
			l.Error().Err(err).Str("node", node.Name).Msg("failed to marshal restore patch")
			continue
		}

		_, err = k.Kube.CoreV1().Nodes().Patch(
			ctx,
			node.Name,
			types.StrategicMergePatchType,
			playloadBytes,
			metav1.PatchOptions{},
		)

		if err != nil {
			l.Error().Err(err).Str("node", node.Name).Msg("failed to patch node restoration")
			continue
		}

		l.Info().Str("node", node.Name).Int64("density", available).Msg("node restored and density updated")
	}
	return nil
}

func (k *KubeClient) DeletePod(ctx context.Context, name string, namespace string) error {
	return k.Kube.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func (k *KubeClient) ListPodsByName(ctx context.Context, namespace string, name string) ([]corev1.Pod, error) {
	podList, err := k.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + name,
	})
	if err != nil {
		return nil, err
	}
	return podList.Items, nil
}

func (k *KubeClient) AddResourceFinalizer(ctx context.Context, hpcJob *v1alpha1.HPCJob) error {
	if strings.Contains(strings.Join(hpcJob.Finalizers, ","), HPC_FINALIZER) {
		return nil
	}
	hpcJob.Finalizers = append(hpcJob.Finalizers, HPC_FINALIZER)
	_, err := k.Kube.
		CoreV1().
		RESTClient().
		Put().
		Namespace(hpcJob.Namespace).
		Resource("hpcjobs").
		Name(hpcJob.Name).
		Body(hpcJob).
		Do(ctx).
		Get()
	return err
}

func (k *KubeClient) RemoveResourceFinalizer(ctx context.Context, hpcJob *v1alpha1.HPCJob) error {
	newFinalizers := []string{}
	for _, f := range hpcJob.Finalizers {
		if f != HPC_FINALIZER {
			newFinalizers = append(newFinalizers, f)
		}
	}
	hpcJob.Finalizers = newFinalizers
	_, err := k.Kube.
		CoreV1().
		RESTClient().
		Put().
		Namespace(hpcJob.Namespace).
		Resource("hpcjobs").
		Name(hpcJob.Name).
		Body(hpcJob).
		Do(ctx).
		Get()
	return err
}
