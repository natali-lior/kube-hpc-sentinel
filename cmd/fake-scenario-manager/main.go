package main

import (
	"log"

	fakescenario "github.com/natali-lior/kube-hpc-sentinel/internal/provisioner/fake-scenario"
)

func main() {
	scenarioManager, err := fakescenario.NewScenarioManager()
	if err != nil {
		log.Fatalf("cloud not initialize scenario manager: %v", err)
	}
	err = scenarioManager.InitializeClusterMap()
	if err != nil {
		log.Fatalf("could not initialize scenario manager cluster map: %v", err)
	}
	scenarioManager.RunScenarioLoop()
}
