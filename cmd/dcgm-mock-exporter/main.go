package main

import (
	"log"

	fakescenario "github.com/natali-lior/kube-hpc-sentinel/internal/provisioner/fake-scenario"
)

func main() {
	if err := fakescenario.ExportMockDCGMMetrics(); err != nil {
		log.Fatalf("could not perform fake scenario export for DCGM mock metrics: %v", err)
	}
}
