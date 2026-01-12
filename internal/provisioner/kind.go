package provisioner

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"slices"
	"time"

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
	if err != nil {
		log.Println("Context: Kind kubeconfig not found, falling back to local config")
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeconfigObj := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		config, err := kubeconfigObj.ClientConfig()
		if err != nil {
			return nil, err
		}
		return kubernetes.NewForConfig(config)
	}
	config, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
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
				return nil
			}
		}
	}
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
	err = k.installKubePrometheusStack(restConfig)
	if err != nil {
		return err
	}
	return nil
}

func (k *KindProvider) installFakeGpuOperator(restConfig *rest.Config) error {
	return k.installChart(restConfig,
		"fake-gpu",
		"https://kindest.github.io/kube-fake-gpu",
		"fake-gpu", "kube-system",
		map[string]any{
			"nodeSelector": map[string]string{"nvidia.com/gpu.present": "true"},
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

func (k *KindProvider) installChart(
	config *rest.Config,
	repoName, repoUrl, chartName, namespace string,
	vals map[string]any) error {
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
	chartPath, err := client.LocateChart(chartName, settings)
	if err != nil {
		return err
	}
	chartRequested, err := loader.Load(chartPath)
	if err != nil {
		return err
	}
	_, err = client.Run(chartRequested, vals)
	return nil
}
