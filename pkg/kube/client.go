package kube

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/natali-lior/kube-hpc-sentinel/pkg/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
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
	kubeconfig := cfg.KubeConfig
	if kubeconfig == "" {
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
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
