package fakescenario

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/natali-lior/kube-hpc-sentinel/pkg/config"
	"github.com/natali-lior/kube-hpc-sentinel/pkg/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ENV_PORT = "PORT"

func ExportMockDCGMMetrics() error {
	cfg := config.Load()
	mapName := os.Getenv(ENV_MAP_NAME)
	if len(mapName) == 0 {
		return fmt.Errorf("env var %s is empty", ENV_MAP_NAME)
	}
	mapNamespace := os.Getenv(ENV_MAP_NAMESPACE)
	if len(mapNamespace) == 0 {
		return fmt.Errorf("env var %s is empty", ENV_MAP_NAMESPACE)
	}
	port := os.Getenv(ENV_PORT)
	if len(port) == 0 {
		return fmt.Errorf("env var %s is empty", ENV_PORT)
	}

	kubeClient, err := kube.NewKubeClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		var sb strings.Builder

		cm, err := kubeClient.Kube.CoreV1().ConfigMaps(mapNamespace).Get(ctx, mapName, metav1.GetOptions{})
		if err != nil {
			log.Printf("Failed to read ConfigMap %s/%s: %v. Serving fallback.", mapNamespace, mapName, err)
			sb.WriteString("# HELP dcgm_exporter_up Status of the mock exporter itself\n")
			sb.WriteString("# TYPE dcgm_exporter_up gauge\n")
			sb.WriteString("dcgm_exporter_up 1\n")
			fmt.Fprint(w, sb.String())
			return
		}

		if len(cm.Data) == 0 {
			log.Printf("ConfigMap %s/%s has no data. Serving fallback.", mapNamespace, mapName)
			sb.WriteString("dcgm_exporter_up 1\n")
			fmt.Fprint(w, sb.String())
			return
		}

		foundMetrics := false
		for nodeName, metricsData := range cm.Data {
			if strings.HasPrefix(nodeName, ".") {
				continue
			}
			sb.WriteString(metricsData)
			sb.WriteString("\n")
			foundMetrics = true
		}

		if !foundMetrics {
			sb.WriteString("dcgm_exporter_up 1\n")
		}

		fmt.Fprint(w, sb.String())
	})

	log.Printf("Mock DCGM exporter starting on port: %s, reading from ConfigMap %s/%s...\n", port, mapNamespace, mapName)
	return http.ListenAndServe(":"+port, nil)
}
