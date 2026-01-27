package controller

import (
	"context"

	kube "github.com/natali-lior/kube-hpc-sentinel/pkg/kube"
	prom "github.com/natali-lior/kube-hpc-sentinel/pkg/prometheus"
	corev1 "k8s.io/api/core/v1"
)

const (
	MULTI_NODE_NVLINK_INDICATOR = "nvidia.com/gpu.clique"
	GPU_NODE_FAMILY_INDICATOR   = "nvidia.com/gpu.family"
)

var (
	NVLINK_ENHANCED_NODES = map[string]bool{
		"hopper": true,
		"ampere": true,
	}
)

func ListGPUNodes(ctx context.Context, kubeClient *kube.KubeClient, promClient *prom.MetricsProvider) ([]corev1.Node, error) {
	gpuNodes, err := kubeClient.GetClusterGPUNodes(ctx)
	if err != nil {
		return nil, err
	}
	healthyNodes := gpuNodes //[]corev1.Node{}
	// healthScan, err := promClient.GetFullGPUClusterHealthCheck(ctx)
	// if err != nil {
	// 	return nil, err
	// }
	// for _, gpuNode := range gpuNodes {
	// 	gpuCores, exists := healthScan[prom.NodeName(gpuNode.Name)]
	// 	if !exists {
	// 		log.Ctx(ctx).Warn().Msgf("gpu node does not export metrics [%v]", gpuNode.Name)
	// 		// todo: metric for node does not export metrics error
	// 		continue
	// 	}

	// }

	return healthyNodes, nil
}

func IsNvlinkCapable(node corev1.Node) bool {
	if _, ok := node.Labels[MULTI_NODE_NVLINK_INDICATOR]; ok {
		return true
	}
	if _, nvlinkEnhanced := NVLINK_ENHANCED_NODES[node.Labels[GPU_NODE_FAMILY_INDICATOR]]; nvlinkEnhanced {
		return true
	}
	return false
}
