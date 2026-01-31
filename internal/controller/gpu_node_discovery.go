package controller

import (
	"context"
	"sync"

	kube "github.com/natali-lior/kube-hpc-sentinel/pkg/kube"
	prom "github.com/natali-lior/kube-hpc-sentinel/pkg/prometheus"
	"github.com/rs/zerolog/log"
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

func ListGPUNodes(ctx context.Context, kubeClient *kube.KubeClient, promClient *prom.MetricsProvider) (healthy []corev1.Node, unhealthy []corev1.Node, err error) {
	gpuNodes, err := kubeClient.GetClusterGPUNodes(ctx)
	if err != nil {
		return nil, nil, err
	}
	healthyNodes := []corev1.Node{}
	unhealthyNodes := []corev1.Node{}
	healthScan, err := promClient.GetFullGPUClusterHealthCheck(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, gpuNode := range gpuNodes {
		gpuCores, exists := healthScan[prom.NodeName(gpuNode.Name)]
		if !exists {
			log.Ctx(ctx).Warn().Msgf("gpu node does not export metrics [%v]", gpuNode.Name)
			// todo: metric for node does not export metrics error
			continue
		}

		healthy := true

		for gpuCore, metrics := range gpuCores {
			for metric, value := range metrics {
				healthRange := prom.HealthMetrics[metric]
				if value < healthRange.Min || value > healthRange.Max {
					log.Ctx(ctx).Warn().Msgf("gpu node %s gpu %s unhealthy metric %s = %f (expected range %f - %f)",
						gpuNode.Name, gpuCore, metric, value, healthRange.Min, healthRange.Max)
					healthy = false
				}
			}
		}
		if healthy {
			healthyNodes = append(healthyNodes, gpuNode)
		} else {
			unhealthyNodes = append(unhealthyNodes, gpuNode)
		}
	}

	return healthyNodes, unhealthyNodes, nil
}

func SyncNodeHealthStatus(ctx context.Context, kubeClient *kube.KubeClient, healthyNodes []corev1.Node, unhealthyNodes []corev1.Node) {
	l := log.Ctx(ctx)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if len(healthyNodes) > 0 {
			l.Info().Int("count", len(healthyNodes)).Msg("starting parallel restoration of healthy nodes")
			if err := kubeClient.RestoreHealthyNodes(ctx, healthyNodes); err != nil {
				l.Error().Err(err).Msg("failed to restore some healthy nodes")
			}
		}
	}()
	go func() {
		defer wg.Done()
		if len(unhealthyNodes) > 0 {
			l.Info().Int("count", len(unhealthyNodes)).Msg("starting parallel isolation of unhealthy nodes")
			if err := kubeClient.IsolateUnhealthyNodes(ctx, unhealthyNodes); err != nil {
				l.Error().Err(err).Msg("failed to isolate some unhealthy nodes")
			}
		}
	}()
	wg.Wait()
	l.Info().Msg("node health sync completed across the cluster")
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
