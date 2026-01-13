package fakescenario

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type ScenarioManager struct {
	Client *kubernetes.Clientset
}

func weightedRand(minH, maxH, minC, maxC float64, chaosWeight float32) float64 {
	if rand.Float32() > chaosWeight {
		return minH + rand.Float64()*(maxH-minH) // Healthy
	}
	return minC + rand.Float64()*(maxC-minC) // Chaos/Surprise event
}

func (s *ScenarioManager) TriggerScenario(nodeName string) error {
	ctx := context.TODO()
	gpuIdx := 0

	cm, err := s.Client.CoreV1().ConfigMaps("monitoring").Get(ctx, "dcgm-mock-data", metav1.GetOptions{})
	if err != nil {
		return err
	}

	// 12% chance for any individual metric to suggest reliability event
	const chaos = 0.12

	metrics := []string{
		// TEMPERATURE (°C)
		// Too High (>85): Thermal Throttling. Operator -> Cordon & Alerts.
		// Too High (>95): Critical. Operator -> Evict Pods to save HW.
		// Too Low (<15): Sensor Failure. Operator -> Mark Node "Unhealthy".
		fmt.Sprintf("dcgm_gpu_temp{node=\"%s\", gpu=\"%d\"} %.0f",
			nodeName, gpuIdx, weightedRand(35, 70, 85, 110, chaos)),

		// MEMORY COPY UTILIZATION (0.0 - 1.0)
		// Too High (>0.9): Bus Saturation. Operator -> Move IO-heavy pods to NVLink nodes.
		// Too Low (<0.05): Starvation. Operator -> Check if CPU/Network is the bottleneck.
		fmt.Sprintf("dcgm_mem_copy_util{node=\"%s\", gpu=\"%d\"} %.2f",
			nodeName, gpuIdx, weightedRand(0.1, 0.6, 0.8, 1.0, chaos)),

		// VRAM USAGE (0.0 - 1.0)
		// Too High (>0.95): OOM Risk. Operator -> Preemptively move pod to higher-VRAM node.
		// Too Low (<0.1): Inefficiency. Operator -> Suggest "Time-Slicing" or "MIG" for this pod.
		fmt.Sprintf("dcgm_fb_usage_gpu{node=\"%s\", gpu=\"%d\"} %.2f",
			nodeName, gpuIdx, weightedRand(0.2, 0.7, 0.9, 0.99, chaos)),

		// ECC ERRORS (Counter)
		// Any Value > 0: Hardware Degrading. Operator -> Cordon node for maintenance.
		// High Value (>10): Fatal. Operator -> Immediately drain node.
		fmt.Sprintf("dcgm_ecc_sbe_aggregate_total{node=\"%s\", gpu=\"%d\"} %.0f",
			nodeName, gpuIdx, weightedRand(0, 0, 1, 13, chaos)),

		// POWER USAGE (Watts)
		// Too High (>400): Power Cap breach. Operator -> Enforce lower clock speeds (Throttling).
		// Too Low (<50): Verification of "Zombie" state if Util is also low.
		fmt.Sprintf("dcgm_power_usage{node=\"%s\", gpu=\"%d\"} %.0f",
			nodeName, gpuIdx, weightedRand(100, 250, 350, 500, chaos)),

		// GPU UTILIZATION (0.0 - 1.0)
		// Too High (1.0): Maxed out. Normal for HPC, but watch for latency increase.
		// Too Low (<0.01): "Zombie Pod". Operator -> Identify pod owner and Reap if idle too long.
		fmt.Sprintf("dcgm_gpu_util{node=\"%s\", gpu=\"%d\"} %.2f",
			nodeName, gpuIdx, weightedRand(0.3, 0.8, 0.0, 1.0, chaos)),

		// NVLINK BANDWIDTH (0 - 400 GB/s)
		// Too Low (<50): Interconnect failure. Operator -> Disallow multi-GPU training jobs on this node.
		fmt.Sprintf("dcgm_nvlink_bandwidth_total{node=\"%s\", gpu=\"%d\"} %.0f",
			nodeName, gpuIdx, weightedRand(320, 400, 0, 80, chaos)),
	}

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data["metrics"] = strings.Join(metrics, "\n") + "\n"

	_, err = s.Client.CoreV1().ConfigMaps("monitoring").Update(ctx, cm, metav1.UpdateOptions{})
	return err
}
