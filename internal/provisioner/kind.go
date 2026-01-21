package provisioner

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/natali-lior/kube-hpc-sentinel/pkg/config"
	"github.com/natali-lior/kube-hpc-sentinel/pkg/kube"
	"go.yaml.in/yaml/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/kind/pkg/cluster"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
)

//go:embed manifests/kind-config.yaml
var kindConfig []byte

const (
	GPU_OPERATOR_NS = "gpu-operator"
	HELM_DRIVER     = "HELM_DRIVER"
	KUBECONFIG      = "KUBECONFIG"
)

type GPUInfo struct {
	Product string
	Family  string
}

var hwCatalog map[string]GPUInfo = map[string]GPUInfo{
	"h100-pool": {"NVIDIA-H100-80GB-HBM3", "hopper"},
	"a100-pool": {"NVIDIA-A100-SXM4-80GB", "ampere"},
	"a800-pool": {"NVIDIA-A800-80GB-SXM4", "ampere"},
	"l4-pool":   {"NVIDIA-L4", "ada"},
	"l40s-pool": {"NVIDIA-L40S", "ada"},
	"t4-pool":   {"Tesla-T4", "turing"},
	"a10-pool":  {"NVIDIA-A10", "ampere"},
	"v100-pool": {"NVIDIA-V100-SXM2-16GB", "volta"},
}

type KindConfigMapping struct {
	Nodes []any `yaml:"nodes"`
}

type KindProvider struct {
	ClusterName string
	Provider    *cluster.Provider
}

func NewKindProvider(name string) *KindProvider {
	return &KindProvider{
		ClusterName: name,
		Provider:    cluster.NewProvider(cluster.ProviderWithDocker()),
	}
}

func (k *KindProvider) CheckSystemRequirements() error {
	checks := []struct {
		name string
		cmd  *exec.Cmd
	}{
		{"Docker", exec.Command("docker", "info")},
		{"Kind", exec.Command("kind", "version")},
		{"Skaffold", exec.Command("skaffold", "version")},
	}

	for _, check := range checks {
		if err := check.cmd.Run(); err != nil {
			return fmt.Errorf("%s check failed: ensure it is installed and running", check.name)
		}
		log.Printf("✓ %s is ready", check.name)
	}
	return nil
}

func (k *KindProvider) Provision() error {
	if err := k.createCluster(); err != nil {
		return fmt.Errorf("failed to create cluster: %w", err)
	}
	return k.waitForNodes()
}

func (k *KindProvider) getExpectedNodeCount() (int, error) {
	var cfg KindConfigMapping
	err := yaml.Unmarshal(kindConfig, &cfg)
	if err != nil {
		return 0, err
	}
	count := len(cfg.Nodes)
	if count == 0 {
		return 1, nil
	}
	return count, nil
}

func (k *KindProvider) createCluster() error {
	clusters, err := k.Provider.List()
	if err != nil {
		return fmt.Errorf("failed to list clusters: %w", err)
	}
	if slices.Contains(clusters, k.ClusterName) {
		log.Printf("Cluster already exists. Skipping creation for %s", k.ClusterName)
		return nil
	}
	log.Printf("Spinning up Kind cluster: %s...", k.ClusterName)
	return k.Provider.Create(
		k.ClusterName,
		cluster.CreateWithRawConfig(kindConfig),
		cluster.CreateWithDisplayUsage(true),
		cluster.CreateWithDisplaySalutation(true),
	)
}

func (k *KindProvider) getKubernetesClient() (*kubernetes.Clientset, error) {
	kubeconfig, err := k.Provider.KubeConfig(k.ClusterName, false)
	if err == nil {
		kClient, err := kube.NewClientFromRawConfig([]byte(kubeconfig))
		if err != nil {
			return nil, fmt.Errorf("failed to create client from kind config: %w", err)
		}
		return kClient.Kube, nil
	}
	cfg := config.Load()
	kClient, err := kube.NewKubeClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("shared loader fallback failed: %w", err)
	}
	return kClient.Kube, nil
}

func (k *KindProvider) waitForNodes() error {
	client, err := k.getKubernetesClient()
	if err != nil {
		return fmt.Errorf("failed to get k8s client: %w", err)
	}
	expectedNodeCount, err := k.getExpectedNodeCount()
	if err != nil {
		return fmt.Errorf("failed to get node count from kind config yaml: %w", err)
	}
	log.Println("Waiting for all nodes to reach 'Ready' status...")
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for nodes to be ready")
		case <-time.After(5 * time.Second):
			nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Error listing nodes: %v", err)
				continue
			}
			readyCount := 0
			for _, node := range nodes.Items {
				for _, cond := range node.Status.Conditions {
					if cond.Type == "Ready" && cond.Status == "True" {
						readyCount++
					}
				}
			}
			log.Printf("... %d/%d nodes ready", readyCount, expectedNodeCount)
			if readyCount >= expectedNodeCount {
				log.Println("All nodes are ready. Cluster is healthy.")
				return k.simulateNvidiaLabels(client)
			}
		}
	}
}

func (k *KindProvider) simulateNvidiaLabels(client *kubernetes.Clientset) error {
	ctx := context.TODO()

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "hpc-sentinel/node-type=gpu-worker",
	})
	if err != nil {
		return err
	}
	for _, node := range nodes.Items {

		lb := k.nvidiaLabels()

		patchData := map[string]any{
			"metadata": map[string]any{
				"labels": lb,
			},
		}
		payloadBytes, err := json.Marshal(patchData)
		if err != nil {
			return err
		}
		_, err = client.CoreV1().Nodes().Patch(
			ctx, node.Name, types.MergePatchType, payloadBytes, metav1.PatchOptions{},
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (k *KindProvider) nvidiaLabels() map[string]string {
	keys := slices.Collect(maps.Keys(hwCatalog))
	poolName := keys[rand.IntN(len(keys))]
	selection := hwCatalog[poolName]

	counts := []int{1, 2, 4, 8}
	randomCount := counts[rand.IntN(len(counts))]

	return map[string]string{
		"hpc-sentinel/node-type":         "gpu-worker",
		"nvidia.com/gpu.count":           fmt.Sprint(randomCount),
		"nvidia.com/gpu.product":         selection.Product,
		"nvidia.com/gpu.machine":         "kind-worker-mock",
		"nvidia.com/gpu.present":         "true",
		"run.ai/simulated-gpu-node-pool": poolName,
		"run.ai/fake.gpu":                "true",
	}
}

func (k *KindProvider) InstallAddons() error {
	kubeconfig, err := k.Provider.KubeConfig(k.ClusterName, false)
	if err != nil {
		return err
	}

	kubeconfigPath := filepath.Join(os.TempDir(), k.ClusterName+"-kubeconfig.yaml")
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0644); err != nil {
		return err
	}
	os.Setenv(KUBECONFIG, kubeconfigPath)
	client, err := k.getKubernetesClient()
	if err != nil {
		return fmt.Errorf("failed to get k8s client: %w", err)
	}
	log.Println("installing cluster addons...")
	if err := k.installFakeGpuOperator(); err != nil {
		return err
	}
	log.Println("fake gpu operator deployed successfully")
	if err = k.restartGPUNodes(client); err != nil {
		return err
	}
	if err = k.waitForGpuCapacity(client); err != nil {
		return err
	}

	if err := k.installKubePrometheusStack(); err != nil {
		return err
	}
	log.Println("prometheus stack deployed successfully")

	return k.launchMockEnvironment()
}

func (k *KindProvider) installFakeGpuOperator() error {
	nodePools := make(map[string]any)
	for poolName, info := range hwCatalog {
		nodePools[poolName] = map[string]any{
			"gpuCount":   8,
			"gpuProduct": info.Product,
			"gpuMemory":  81920,
		}
	}
	return k.installChart(
		"run-ai",
		"oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator",
		"fake-gpu-operator",
		GPU_OPERATOR_NS,
		"0.0.68",
		map[string]any{
			"nodeSelector": map[string]string{"nvidia.com/gpu.present": "true"},
			"privileged":   true,
			"tolerations": []map[string]any{
				{
					"key":      "nvidia.com/gpu",
					"operator": "Equal",
					"value":    "true",
					"effect":   "NoSchedule",
				},
			},
			"topology": map[string]any{
				"nodePools": nodePools,
			},
			"metrics": map[string]any{
				"enabled": true,
			},
		},
	)
}

func (k *KindProvider) installKubePrometheusStack() error {
	promValues := map[string]any{
		"prometheus": map[string]any{
			"nodeSelector": map[string]string{"hpc-sentinel/node-type": "observability"},
			"tolerations": []map[string]any{
				{"key": "node-role.kubernetes.io/observability", "operator": "Exists", "effect": "NoSchedule"},
			},
		},
		"grafana": map[string]any{
			"nodeSelector": map[string]string{"hpc-sentinel/node-type": "observability"},
			"tolerations": []map[string]any{
				{"key": "node-role.kubernetes.io/observability", "operator": "Exists", "effect": "NoSchedule"},
			},
			"sidecar": map[string]any{
				"enabled":         true,
				"label":           "grafana_dashboard",
				"labelValue":      "1",
				"searchNamespace": "ALL",
			},
		},
		"kubeEtcd": map[string]any{
			"enabled": false,
		},
		"kubeProxy": map[string]any{
			"enabled": false,
		},
		"kubeControllerManager": map[string]any{
			"enabled":   true,
			"endpoints": []string{},
			"service": map[string]any{
				"enabled":    true,
				"port":       10257,
				"targetPort": 10257,
			},
			"serviceMonitor": map[string]any{
				"https":                true,
				"insecuritySkipVerify": true,
			},
		},
		"kubeScheduler": map[string]any{
			"enabled": true,
			"service": map[string]any{
				"enabled":    true,
				"port":       10259,
				"targetPort": 10259,
			},
			"serviceMonitor": map[string]any{
				"https":              true,
				"insecureSkipVerify": true,
			},
		},
	}
	return k.installChart(
		"prometheus-community",
		"https://prometheus-community.github.io/helm-charts",
		"kube-prometheus-stack", "monitoring", "", promValues,
	)
}

func (k *KindProvider) isChartInstalled(namespace, releaseName string) bool {
	settings := cli.New()
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, "secret", func(format string, v ...any) {}); err != nil {
		return false
	}
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	_, err := histClient.Run(releaseName)
	return err == nil
}

func (k *KindProvider) installChart(
	repoName, repoUrl, chartName, namespace, version string,
	vals map[string]any) error {

	if k.isChartInstalled(namespace, chartName) {
		log.Printf("Release '%s' already exists. Skipping.", chartName)
		return nil
	}

	valsFile := filepath.Join(os.TempDir(), chartName+"-values.yaml")
	valsYaml, _ := yaml.Marshal(vals)
	_ = os.WriteFile(valsFile, valsYaml, 0644)
	defer os.Remove(valsFile)

	chartRef := repoUrl
	if !strings.HasPrefix(repoUrl, "oci://") {
		chartRef = fmt.Sprintf("%s/%s", repoName, chartName)
	}

	args := []string{
		"upgrade", "--install", chartName, chartRef,
		"--namespace", namespace,
		"--create-namespace",
		"--values", valsFile,
		"--wait",
		"--timeout", "15m",
	}

	if version != "" && version != "latest" {
		args = append(args, "--version", version)
	}

	if !strings.HasPrefix(repoUrl, "oci://") {
		addRepo := exec.Command("helm", "repo", "add", repoName, repoUrl)
		_ = addRepo.Run()
		_ = exec.Command("helm", "repo", "update").Run()
	}

	log.Printf("Installing %s via Helm CLI...", chartName)
	cmd := exec.Command("helm", args...)

	kubeconfigPath := filepath.Join(os.TempDir(), k.ClusterName+"-kubeconfig.yaml")
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigPath)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("helm install failed: %s: %w", string(out), err)
	}

	return nil
}

func (k *KindProvider) launchMockEnvironment() error {
	log.Println("Cleaning up any existing skaffold processes...")
	killCmd := exec.Command("pkill", "-f", "skaffold")
	_ = killCmd.Run()

	log.Println("starting skaffold dev mode for continuous deployment...")

	kubeconfigPath := filepath.Join(os.TempDir(), k.ClusterName+"-kubeconfig.yaml")

	cmd := exec.Command("skaffold", "dev",
		"--cache-artifacts=false",
		"--force",
		"--trigger=polling",
		"--port-forward=user")
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = "."

	log.Println("Skaffold will now watch for changes and auto-reload...")
	log.Println("Tip: If auto-reload stops working, manually restart skaffold:")
	log.Printf("  KUBECONFIG=%s skaffold dev --force --trigger=polling --port-forward=user\n", kubeconfigPath)
	return cmd.Start()
}

func (k *KindProvider) restartGPUNodes(client *kubernetes.Clientset) error {
	log.Println("Restarting GPU nodes to force-refresh resource capacity...")
	ctx := context.TODO()

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "hpc-sentinel/node-type=gpu-worker",
	})
	if err != nil {
		return fmt.Errorf("failed to list gpu nodes for restart: %w", err)
	}

	for _, node := range nodes.Items {
		log.Printf("Restarting Docker container for node: %s", node.Name)
		cmd := exec.Command("docker", "restart", node.Name)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to restart container %s: %s", node.Name, string(out))
		}
		time.Sleep(3 * time.Second)
	}
	return k.waitForNodes()
}

func (k *KindProvider) waitForGpuCapacity(client *kubernetes.Clientset) error {
	log.Println("Waiting for Kubelet to register GPU capacity...")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for GPU capacity to register")
		case <-time.After(10 * time.Second):
			nodes, _ := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
				LabelSelector: "hpc-sentinel/node-type=gpu-worker",
			})

			allReady := true
			for _, node := range nodes.Items {
				qty := node.Status.Capacity["nvidia.com/gpu"]
				if qty.Value() == 0 {
					allReady = false
					log.Printf("... Node %s still reporting 0 GPUs", node.Name)
					break
				}
			}
			if allReady && len(nodes.Items) > 0 {
				log.Println("✓ All GPU workers reporting capacity.")
				return nil
			}
		}
	}
}
