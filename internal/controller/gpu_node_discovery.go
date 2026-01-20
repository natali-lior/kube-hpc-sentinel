package controller

import (
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

func IsNvlinkCapable(node corev1.Node) bool {
	if _, ok := node.Labels[MULTI_NODE_NVLINK_INDICATOR]; ok {
		return true
	}
	if _, nvlinkEnhanced := NVLINK_ENHANCED_NODES[node.Labels[GPU_NODE_FAMILY_INDICATOR]]; nvlinkEnhanced {
		return true
	}
	return false
}
