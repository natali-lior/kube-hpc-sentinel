package main

import (
	"log"

	"github.com/natali-lior/kube-hpc-sentinel/internal/provisioner"
)

var (
	ENV    = "ENV"
	LOCAL  = "LOCAL"
	REMOTE = "REMOTE"
)

func main() {
	p := provisioner.NewKindProvider("hpc-sentinel-local")
	if err := p.Provision(); err != nil {
		log.Fatalf("setup failed: %v", err)
	}
	if err := p.InstallAddons(); err != nil {
		log.Fatalf("failed to install addons: %v", err)
	}
}
