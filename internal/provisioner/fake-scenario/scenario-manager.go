package fakescenario

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/natali-lior/kube-hpc-sentinel/pkg/config"
	"github.com/natali-lior/kube-hpc-sentinel/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	ENV_MAP_NAME      = "MAP_NAME"
	ENV_MAP_NAMESPACE = "MAP_NAMESPACE"

	GPU_COUNT_LABEL = "nvidia.com/gpu.count"
)

var (
	nodeNamesToGPUs = map[string][]string{}
)

type ScenarioManager struct {
	client *kubernetes.Clientset
}

func NewScenarioManager() (*ScenarioManager, error) {
	cfg := config.Load()
	kClient, err := kube.NewKubeClient(cfg)
	if err != nil {
		return nil, err
	}
	return &ScenarioManager{
		client: kClient.Kube,
	}, nil
}

func (s *ScenarioManager) InitializeClusterMap() error {
	log.Println("Scanning cluster for GPU nodes...")
	ctx := context.Background()

	nodes, err := s.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "hpc-sentinel/node-type=gpu-worker",
	})
	if err != nil {
		return fmt.Errorf("failed to list gpu nodes: %w", err)
	}

	for _, node := range nodes.Items {
		countStr, ok := node.Labels[GPU_COUNT_LABEL]
		if !ok {
			log.Printf("warning: node %s missing '%s' label", node.Name, GPU_COUNT_LABEL)
			continue
		}

		var count int
		_, err := fmt.Sscanf(countStr, "%d", &count)
		if err != nil {
			log.Printf("error parsing '%s' on %s: %v", node.Name, GPU_COUNT_LABEL, err)
			continue
		}

		var gpuIDs []string
		for i := 0; i < count; i++ {
			gpuIDs = append(gpuIDs, fmt.Sprintf("%d", i))
		}
		nodeNamesToGPUs[node.Name] = gpuIDs
		log.Printf("Registered Node %s: %d simulated GPUs", node.Name, count)
	}

	if len(nodeNamesToGPUs) == 0 {
		return fmt.Errorf("no GPU nodes found with expected labels")
	}
	return nil
}

func weightedRand(minH, maxH, minC, maxC float64, chaosWeight float32) float64 {
	r := rand.Float32()
	fmt.Printf("random event weight: %v\n", r)
	if r > chaosWeight {
		return minH + rand.Float64()*(maxH-minH)
	}
	fmt.Printf("chaos event trigger\n")
	return minC + rand.Float64()*(maxC-minC)
}

func (s *ScenarioManager) generateMetrics(nodeName string, gpuId string) string {
	const chaos = 0.02
	metrics := []string{
		/* gpu temperature:
		too high: gpu slows clocks speed, makes the node x2-x3 times slower,
		decision: taint node to avoid more hpc jobs
		*/
		fmt.Sprintf("dcgm_gpu_temp{node=\"%s\", gpu=\"%s\"} %.0f", nodeName, gpuId, weightedRand(35, 70, 85, 110, chaos)),
		/* memory bandwidth: time spent moving data between cpu(RAM) to GPU(vRAM),
		too high: bottleneck in data loading, gpu spends more time waiting than performing
		too low: if job is supposedly running, maybe the pipeline is stuck
		decision: if too high taint as this node will throttle open job, if too low then check pipeline health
		*/
		fmt.Sprintf("dcgm_mem_copy_util{node=\"%s\", gpu=\"%s\"} %.2f", nodeName, gpuId, weightedRand(0.1, 0.6, 0.8, 1.0, chaos)),
		/* VRAM fill: frame buffer usage, how much of the memory is occupied
		too high: upcoming OOM
		too low: under-utilization
		decision: if too high prepare for pipeline failure due to OOM, instruct the hpc to tolerate different topology taint, too low: occupy with job that can fit the topology
		*/
		fmt.Sprintf("dcgm_fb_usage_gpu{node=\"%s\", gpu=\"%s\"} %.2f", nodeName, gpuId, weightedRand(0.2, 0.7, 0.9, 0.99, chaos)),
		/* HW Error
		to high: upcoming kernel panic / hw failure
		decision: taint the node to "quarantine" as long as it larger than 0, proactively drain if continues
		*/
		fmt.Sprintf("dcgm_ecc_sbe_aggregate_total{node=\"%s\", gpu=\"%s\"} %.0f", nodeName, gpuId, weightedRand(0, 0, 0, 3, chaos)),
		/* Power consumption in Watts
		too high: Power capping/PSU failure, if compute high - overutilized, if compute low - a zombie process / hw short
		too low: idle gpu
		decision: if too high then taint, if too low then check ecc/sbe errors or search for the zombie process that is occupying the node
		*/
		fmt.Sprintf("dcgm_power_usage{node=\"%s\", gpu=\"%s\"} %.0f", nodeName, gpuId, weightedRand(100, 250, 350, 500, chaos)),
		/* The percentage of time the GPU kernels were active in the last sample
		high util + high power: good
		low util + high memCopy: the GPU is starving for data
		high util + low power: inefficient kernel, might slow down pipelines
		decision: follow up after pipeline slowdown, do not assign again the job to expensive node if fails
		*/
		fmt.Sprintf("dcgm_gpu_util{node=\"%s\", gpu=\"%s\"} %.2f", nodeName, gpuId, weightedRand(0.3, 0.8, 0.0, 1.0, chaos)),
		/* The speed of private network between GPUs
		too low: GPU cannot talk to each other, distributed training jobs will hang forever or crash with NCCL_ERROR
		decision: taint the nodes of the rack, they are out of use until fixed
		*/
		fmt.Sprintf("dcgm_nvlink_bandwidth_total{node=\"%s\", gpu=\"%s\"} %.0f", nodeName, gpuId, weightedRand(250, 400, 0, 80, chaos)),
	}
	return strings.Join(metrics, "\n")
}

func (s *ScenarioManager) RunScenarioLoop() {
	ticker := time.NewTicker(10 * time.Second)
	ctx := context.Background()

	for range ticker.C {
		log.Println("performing reliability scenario...")
		newData := make(map[string]string)
		for node, gpus := range nodeNamesToGPUs {
			var nodeMetrics []string
			for _, gpu := range gpus {
				nodeMetrics = append(nodeMetrics, s.generateMetrics(node, gpu))
			}
			newData[node] = strings.Join(nodeMetrics, "\n") + "\n"
		}
		if err := s.syncMetrics(ctx, newData); err != nil {
			log.Printf("failed to sync metrics: %v", err)
		}
	}
}

func (s *ScenarioManager) syncMetrics(ctx context.Context, data map[string]string) error {
	name := os.Getenv(ENV_MAP_NAME)
	if len(name) == 0 {
		return fmt.Errorf("config map name for metrics sync must be provided [%s]", ENV_MAP_NAME)
	}
	ns := os.Getenv(ENV_MAP_NAMESPACE)
	if len(ns) == 0 {
		return fmt.Errorf("config map namespace for metrics sync must be provided [%s]", ENV_MAP_NAMESPACE)
	}
	cm, err := s.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			log.Printf("config map %s not found, it will be created by sync loop...", name)
			newCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns,
				},
				Data: data,
			}
			_, err = s.client.CoreV1().ConfigMaps(ns).Create(ctx, newCM, metav1.CreateOptions{})
		}
		return err
	}
	cm.Data = data
	_, err = s.client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}
