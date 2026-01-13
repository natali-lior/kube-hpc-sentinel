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
		entries, err := os.ReadDir(metricsDir)
		if err != nil {
			http.Error(w, "could not read metrics directory", http.StatusInternalServerError)
			return
		}

		var sb strings.Builder
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
		}

		fmt.Fprint(w, sb.String())
	})

	log.Printf("Mock DCGM exporter starting on port: %s...\n", port)
	return http.ListenAndServe(":"+port, nil)
}
