package kube

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/natali-lior/kube-hpc-sentinel/pkg/config"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	GPU_NODE_LABEL_INDICATOR = "nvidia.com/gpu.count"
)

type KubeClient struct {
	Kube   *kubernetes.Clientset
	Config *rest.Config
}

func NewKubeClient(cfg *config.Config) (*KubeClient, error) {
	config, err := GetConfig(cfg)
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return &KubeClient{
		Kube:   clientset,
		Config: config,
	}, nil
}

func GetConfig(cfg *config.Config) (*rest.Config, error) {
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}
	if cfg.KubeConfig == "" {
		if home := homedir.HomeDir(); home != "" {
			cfg.KubeConfig = filepath.Join(home, ".kube", "config")
		}
	}
	if _, err := os.Stat(cfg.KubeConfig); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubeconfig not found at %s", cfg.KubeConfig)
	}
	return clientcmd.BuildConfigFromFlags("", cfg.KubeConfig)
}

func NewClientFromRawConfig(raw []byte) (*KubeClient, error) {
	config, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &KubeClient{Kube: clientset, Config: config}, nil
}

func (k *KubeClient) GetClusterGPUNodes(ctx context.Context) ([]corev1.Node, error) {
	l := log.Ctx(ctx)
	gpuLabelExists, err := labels.NewRequirement(GPU_NODE_LABEL_INDICATOR, selection.Exists, nil)
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*gpuLabelExists)
	nodes, err := k.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}
	var validNodes []corev1.Node
	for _, node := range nodes.Items {
		countStr := node.Labels[GPU_NODE_LABEL_INDICATOR]
		count, err := strconv.Atoi(countStr)
		if err != nil || count <= 0 {
			l.Warn().Str("node", node.Name).Str("val", countStr).Msg("skipping node: invalid GPU count label")
			continue
		}
		validNodes = append(validNodes, node)
	}
	l.Info().Int("count", len(validNodes)).Msg("successfully discovered GPU nodes")
	return validNodes, nil
}
