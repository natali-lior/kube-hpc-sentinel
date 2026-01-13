package fakescenario

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	ENV_METRICS_DIR_PATH = "METRICS_DIR_PATH"
	ENV_PORT             = "PORT"
)

func ExportMockDCGMMetrics() error {
	metricsDir := os.Getenv(ENV_METRICS_DIR_PATH)
	if len(metricsDir) == 0 {
		return fmt.Errorf("env var %s is empty", ENV_METRICS_DIR_PATH)
	}
	port := os.Getenv(ENV_PORT)
	if len(port) == 0 {
		return fmt.Errorf("env var %s is empty", ENV_PORT)
	}

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		var sb strings.Builder
		entries, err := os.ReadDir(metricsDir)
		if err != nil || len(entries) == 0 {
			log.Printf("Metrics directory %s empty or unreadable. Serving fallback status.", metricsDir)
			sb.WriteString("# HELP dcgm_exporter_up Status of the mock exporter itself\n")
			sb.WriteString("# TYPE dcgm_exporter_up gauge\n")
			sb.WriteString("dcgm_exporter_up 1\n")
			fmt.Fprint(w, sb.String())
			return
		}
		foundMetrics := false
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}

			fullPath := filepath.Join(metricsDir, entry.Name())
			data, err := os.ReadFile(fullPath)
			if err != nil {
				log.Printf("failed to read node metrics for %s: %v", entry.Name(), err)
				continue
			}

			sb.Write(data)
			sb.WriteString("\n")
			foundMetrics = true
		}
		if !foundMetrics {
			sb.WriteString("dcgm_exporter_up 1\n")
		}

		fmt.Fprint(w, sb.String())
	})

	log.Printf("Mock DCGM exporter starting on port: %s...\n", port)
	return http.ListenAndServe(":"+port, nil)
}
