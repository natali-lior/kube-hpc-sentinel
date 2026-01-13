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
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/natali-lior/kube-hpc-sentinel/pkg/kube"
	"go.yaml.in/yaml/v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/kind/pkg/cluster"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
)

//go:embed manifests/kind-config.yaml
var kindConfig []byte

const (
	HELM_DRIVER = "HELM_DRIVER"
	KUBECONFIG  = "KUBECONFIG"
)

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
	kClient, err := kube.NewKubeClient()
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
	randomCount := rand.IntN(8) + 1
	processors := []string{
		"NVIDIA-H100-80GB-HBM3",
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
		if newLabels == nil {
			newLabels = make(map[string]string)
		}
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

	kubeconfigPath := filepath.Join(os.TempDir(), k.ClusterName+"-kubeconfig.yaml")
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0644); err != nil {
		return err
	}
	os.Setenv(KUBECONFIG, kubeconfigPath)

	log.Println("installing cluster addons...")
	if err := k.installFakeGpuOperator(); err != nil {
		return err
	}
	log.Println("fake gpu operator deployed successfully")

	if err := k.installKubePrometheusStack(); err != nil {
		return err
	}
	log.Println("prometheus stack deployed successfully")

	return k.launchMockEnvironment()
}

func (k *KindProvider) createFakeTopologies() error {
	ctx := context.TODO()
	client, err := k.getKubernetesClient()
	if err != nil {
		return err
	}
	_, _ = client.CoreV1().Namespaces().Create(ctx, &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu-operator"},
	}, metav1.CreateOptions{})

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "hpc-sentinel/node-type=gpu-worker",
	})
	if err != nil {
		return err
	}

	for _, node := range nodes.Items {
		cmName := fmt.Sprintf("topology-%s", node.Name)
		log.Printf("Creating mock topology for GPU node: %s", node.Name)

		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: "gpu-operator",
			},
			Data: map[string]string{
				"topology.yaml": "nodes: []",
			},
		}

		_, err := client.CoreV1().ConfigMaps("gpu-operator").Create(ctx, cm, metav1.CreateOptions{})
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			log.Printf("Warning: failed to create configmap %s: %v", cmName, err)
		}
	}
	return nil
}

func (k *KindProvider) installFakeGpuOperator() error {
	err := k.createFakeTopologies()
	if err != nil {
		return fmt.Errorf("could not perform creation of fake topology as pre step for fake-gpu-operator: %w", err)
	}
	return k.installChart(
		"run-ai",
		"oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator",
		"fake-gpu-operator",
		"gpu-operator",
		"0.0.68",
		map[string]any{
			"nodeSelector": map[string]string{"nvidia.com/gpu.present": "true"},
			"privileged":   true,
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
	log.Println("starting skaffold for mock apps...")
	cmd := exec.Command("skaffold", "run", "--tail")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
