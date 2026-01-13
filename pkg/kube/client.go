package kube

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	KUBECONFIG = "KUBECONFIG"
)

type KubeClient struct {
	Kube   *kubernetes.Clientset
	Config *rest.Config
}

func NewKubeClient() (*KubeClient, error) {
	config, err := GetConfig()
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

func GetConfig() (*rest.Config, error) {
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}
	kubeconfig := os.Getenv(KUBECONFIG)
	if kubeconfig == "" {
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}
	if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubeconfig not found at %s", kubeconfig)
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
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
