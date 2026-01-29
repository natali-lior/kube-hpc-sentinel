package config

import (
	"os"
	"path/filepath"

	"k8s.io/client-go/util/homedir"
)

const (
	ENV_PORT               = "PORT"
	ENV_LOG_LEVEL          = "LOG_LEVEL"
	ENV_KUBECONFIG         = "KUBECONFIG"
	ENV_PROMETHEUS_ADDRESS = "PROMETHEUS_ADDRESS"
)

const (
	INFO = "info"
)

const (
	DEFAULT_PORT               = "8080"
	DEFAULT_LOG_LEVEL          = INFO
	DEFAULT_PROMETHEUS_ADDRESS = "http://localhost:9090"
)

type Config struct {
	Port              string
	LogLevel          string
	KubeConfig        string
	PrometheusAddress string
}

func Load() *Config {
	return &Config{
		Port:              getEnv(ENV_PORT, DEFAULT_PORT),
		LogLevel:          getEnv(ENV_LOG_LEVEL, DEFAULT_LOG_LEVEL),
		KubeConfig:        getEnv(ENV_KUBECONFIG, filepath.Join(homedir.HomeDir(), ".kube", "config")),
		PrometheusAddress: getEnv(ENV_PROMETHEUS_ADDRESS, DEFAULT_PROMETHEUS_ADDRESS),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
