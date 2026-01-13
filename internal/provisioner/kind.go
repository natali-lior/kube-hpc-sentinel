package provisioner

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"maps"
	"math/rand/v2"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/natali-lior/kube-hpc-sentinel/pkg/kube"
	"go.yaml.in/yaml/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/cluster"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/rest"
)

//go:embed manifests/kind-config.yaml
var kindConfig []byte

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

func (k *KindProvider) GetKubernetesClient() (*kubernetes.Clientset, error) {
	kubeconfig, err := k.Provider.KubeConfig(k.ClusterName, false)
	if err == nil {
		kClient, err := kube.NewClientFromRawConfig([]byte(kubeconfig))
		if err != nil {
			return nil, fmt.Errorf("failed to create client from kind config: %w", err)
		}
		return kClient.Kube, nil
	}
	log.Println("context: kind-specific kubeconfig not found, falling back to shared loader")
	kClient, err := kube.NewKubeClient()
	if err != nil {
		return nil, fmt.Errorf("shared loader fallback failed: %w", err)
	}
	return kClient.Kube, nil
}

func (k *KindProvider) waitForNodes() error {
	client, err := k.GetKubernetesClient()
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
	randomCount := rand.IntN(8) + 1
	processors := []string{
		"NVIDIA-H100-80GB-HBM3",
		"NVIDIA-A100-SXM4-80GB",
		"NVIDIA-A100-SXM4-80GB",
		"NVIDIA-A800-80GB-SXM4",
		"NVIDIA-L4",
		"NVIDIA-L40S",
		"Tesla-T4",
		"NVIDIA-A10",
	}
	processorIdx := rand.IntN(len(processors))
	nvidiaLabels := map[string]string{
		"gpu-count":                           fmt.Sprint(randomCount),
		"nvidia.com/gpu.count":                fmt.Sprint(randomCount),
		"nvidia.com/gpu.family":               "ampere",
		"nvidia.com/gpu.machine":              "kind-worker-mock",
		"nvidia.com/gpu.present":              "true",
		"nvidia.com/gpu.product":              processors[processorIdx],
		"nvidia.com/cuda.driver.major":        "525",
		"nvidia.com/cuda.driver.minor":        "60",
		"nvidia.com/gpu.deploy.device-plugin": "true",
		"nvidia.com/gpu.deploy.dcgm-exporter": "true",
	}
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "hpc-sentinel/node-type=gpu-worker",
	})
	if err != nil {
		return err
	}
	for _, node := range nodes.Items {
		newLabels := node.Labels
		maps.Copy(newLabels, nvidiaLabels)
		node.SetLabels(newLabels)
		_, err := client.CoreV1().Nodes().Update(ctx, &node, metav1.UpdateOptions{})
		if err != nil {
			log.Printf("failed to label node %s: %v", node.Name, err)
		}
	}
	return nil
}

func (k *KindProvider) InstallAddons() error {
	kubeconfig, err := k.Provider.KubeConfig(k.ClusterName, false)
	if err != nil {
		return err
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		return err
	}
	log.Println("installing cluster addons...")
	err = k.installFakeGpuOperator(restConfig)
	if err != nil {
		return err
	}
	log.Println("fake gpu operator deployed successfully")
	err = k.installKubePrometheusStack(restConfig)
	if err != nil {
		return err
	}
	log.Println("prometheus stack deployed successfully")
	return k.launchMockEnvironment()
}

func (k *KindProvider) installFakeGpuOperator(restConfig *rest.Config) error {
	return k.installChart(restConfig,
		"fake-gpu-operator",
		"oci://ghcr.io/run-ai/fake-gpu-operator",
		"fake-gpu-operator",
		"gpu-operator",
		map[string]any{
			"nodeSelector": map[string]string{"nvidia.com/gpu.present": "true"},
			"privileged":   true,
			"metrics": map[string]any{
				"enabled": true,
			},
		},
	)
}

func (k *KindProvider) installKubePrometheusStack(restConfig *rest.Config) error {
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
		},
	}
	return k.installChart(restConfig,
		"prometheus-community",
		"https://prometheus-community.github.io/helm-charts",
		"kube-prometheus-stack", "monitoring", promValues,
	)
}

func (k *KindProvider) isChartInstalled(namespace, releaseName string) bool {
	settings := cli.New()
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), func(format string, v ...any) {}); err != nil {
		return false
	}
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	_, err := histClient.Run(releaseName)
	return err == nil
}

func (k *KindProvider) installChart(
	config *rest.Config,
	repoName, repoUrl, chartName, namespace string,
	vals map[string]any) error {

	if k.isChartInstalled(namespace, chartName) {
		log.Printf("Release '%s' already exists in namespace '%s'. Skipping helm install", chartName, namespace)
		return nil
	}

	settings := cli.New()
	actionConfig := new(action.Configuration)
	err := actionConfig.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), log.Printf)
	if err != nil {
		return err
	}
	client := action.NewInstall(actionConfig)
	client.ReleaseName = chartName
	client.Namespace = namespace
	client.CreateNamespace = true
	client.Wait = true
	client.Timeout = 5 * time.Minute

	var chartPath string
	if strings.HasPrefix(repoUrl, "oci://") {
		chartPath, err = client.LocateChart(repoUrl, settings)
	} else {
		client.ChartPathOptions.RepoURL = repoUrl
		chartPath, err = client.LocateChart(chartName, settings)
	}
	if err != nil {
		return fmt.Errorf("failed to locate chart: %w", err)
	}
	chartRequested, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("failed to load chart: %w", err)
	}
	_, err = client.Run(chartRequested, vals)
	return nil
}

func (k *KindProvider) launchMockEnvironment() error {
	log.Println("starting skaffold for mock apps...")
	cmd := exec.Command("skaffold", "run", "--tail")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
