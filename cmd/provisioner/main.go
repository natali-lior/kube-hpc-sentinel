package main

import (
	"log"
	"os"

	"github.com/natali-lior/kube-hpc-sentinel/internal/provisioner"
)

var (
	ENV   = "ENV"
	LOCAL = "LOCAL"
)

func main() {
	var p provisioner.ClusterProvider
	switch os.Getenv(ENV) {
	case LOCAL:
		p = provisioner.NewKindProvider("hpc-sentinel-local")
	default:
		p = provisioner.NewKindProvider("hpc-sentinel-local")
	}

	if err := p.Provision(); err != nil {
		log.Fatalf("setup failed: %v", err)
	}
	if err := p.InstallAddons(); err != nil {
		log.Fatalf("failed to install addons: %v", err)
	}
}
