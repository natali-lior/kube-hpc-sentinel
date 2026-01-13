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

	"github.com/natali-lior/kube-hpc-sentinel/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	ENV_MAP_NAME      = "MAP_NAME"
	ENV_MAP_NAMESPACE = "MAP_NAMESPACE"
)

var (
	nodeNamesToGPUs = map[string][]string{}
)

type ScenarioManager struct {
	client *kubernetes.Clientset
}

func NewScenarioManager() (*ScenarioManager, error) {
	kClient, err := kube.NewKubeClient()
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
		countStr, ok := node.Labels["gpu-count"]
		if !ok {
			log.Printf("warning: node %s missing 'gpu-count' label", node.Name)
			continue
		}

		var count int
		_, err := fmt.Sscanf(countStr, "%d", &count)
		if err != nil {
			log.Printf("error parsing gpu-count on %s: %v", node.Name, err)
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
	if rand.Float32() > chaosWeight {
		return minH + rand.Float64()*(maxH-minH)
	}
	return minC + rand.Float64()*(maxC-minC)
}

func (s *ScenarioManager) generateMetrics(nodeName string, gpuId string) string {
	const chaos = 0.12
	metrics := []string{
		fmt.Sprintf("dcgm_gpu_temp{node=\"%s\", gpu=\"%s\"} %.0f", nodeName, gpuId, weightedRand(35, 70, 85, 110, chaos)),
		fmt.Sprintf("dcgm_mem_copy_util{node=\"%s\", gpu=\"%s\"} %.2f", nodeName, gpuId, weightedRand(0.1, 0.6, 0.8, 1.0, chaos)),
		fmt.Sprintf("dcgm_fb_usage_gpu{node=\"%s\", gpu=\"%s\"} %.2f", nodeName, gpuId, weightedRand(0.2, 0.7, 0.9, 0.99, chaos)),
		fmt.Sprintf("dcgm_ecc_sbe_aggregate_total{node=\"%s\", gpu=\"%s\"} %.0f", nodeName, gpuId, weightedRand(0, 0, 1, 13, chaos)),
		fmt.Sprintf("dcgm_power_usage{node=\"%s\", gpu=\"%s\"} %.0f", nodeName, gpuId, weightedRand(100, 250, 350, 500, chaos)),
		fmt.Sprintf("dcgm_gpu_util{node=\"%s\", gpu=\"%s\"} %.2f", nodeName, gpuId, weightedRand(0.3, 0.8, 0.0, 1.0, chaos)),
		fmt.Sprintf("dcgm_nvlink_bandwidth_total{node=\"%s\", gpu=\"%s\"} %.0f", nodeName, gpuId, weightedRand(320, 400, 0, 80, chaos)),
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
