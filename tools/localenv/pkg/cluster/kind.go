package cluster

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const kindConfigTemplate = `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: %s
nodes:
  - role: control-plane
%s
`

const workerNodeTemplate = `  - role: worker
    labels:
      node-type: worker
`

// CreateCluster creates a Kind cluster with the specified number of workers
func CreateCluster(ctx context.Context, name string, workers int, verbose bool) error {
	// Check if cluster already exists
	exists, err := ClusterExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("cluster '%s' already exists", name)
	}

	// Generate Kind config
	workerNodes := strings.Repeat(workerNodeTemplate, workers)
	config := fmt.Sprintf(kindConfigTemplate, name, workerNodes)

	// Write config to temp file
	configFile, err := os.CreateTemp("", "kind-config-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp config: %w", err)
	}
	defer os.Remove(configFile.Name())

	if _, err := configFile.WriteString(config); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	configFile.Close()

	// Create cluster
	cmd := exec.CommandContext(ctx, "kind", "create", "cluster", "--config", configFile.Name())
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind create cluster failed: %w", err)
	}

	return nil
}

// DeleteCluster deletes the Kind cluster
func DeleteCluster(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "kind", "delete", "cluster", "--name", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind delete cluster failed: %w", err)
	}

	return nil
}

// ClusterExists checks if a Kind cluster exists
func ClusterExists(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "kind", "get", "clusters")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to list clusters: %w", err)
	}

	clusters := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, cluster := range clusters {
		if cluster == name {
			return true, nil
		}
	}

	return false, nil
}

// LabelGPUNodes labels the first N worker nodes as GPU nodes
func LabelGPUNodes(ctx context.Context, clusterName string, gpuNodeCount int) error {
	// Switch to the cluster context
	contextName := "kind-" + clusterName
	if err := switchContext(ctx, contextName); err != nil {
		return err
	}

	// Get worker nodes
	cmd := exec.CommandContext(ctx, "kubectl", "get", "nodes",
		"-l", "node-role.kubernetes.io/control-plane!=true",
		"-o", "jsonpath={.items[*].metadata.name}")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get worker nodes: %w", err)
	}

	workers := strings.Fields(string(output))
	if len(workers) < gpuNodeCount {
		return fmt.Errorf("requested %d GPU nodes but only %d workers available", gpuNodeCount, len(workers))
	}

	// Label GPU nodes
	for i := 0; i < gpuNodeCount; i++ {
		nodeName := workers[i]
		labels := []string{
			"nvidia.com/gpu.present=true",
			"nvidia.com/gpu.count=8", // Fake 8 GPUs per node
			"nvidia.com/gpu.product=NVIDIA-A100-SXM4-80GB",
			"node-type=gpu-worker",
		}

		for _, label := range labels {
			cmd := exec.CommandContext(ctx, "kubectl", "label", "node", nodeName, label, "--overwrite")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to label node %s: %w", nodeName, err)
			}
		}

		// Add taints so only GPU workloads are scheduled here
		cmd = exec.CommandContext(ctx, "kubectl", "taint", "node", nodeName,
			"nvidia.com/gpu=present:NoSchedule", "--overwrite")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to taint node %s: %w", nodeName, err)
		}

		fmt.Printf("   ✓ Labeled node %s as GPU node (8x A100 80GB)\n", nodeName)
	}

	return nil
}

// ShowClusterInfo displays cluster information
func ShowClusterInfo(ctx context.Context, name string) error {
	contextName := "kind-" + name
	if err := switchContext(ctx, contextName); err != nil {
		return err
	}

	// Get nodes
	cmd := exec.CommandContext(ctx, "kubectl", "get", "nodes", "-o", "wide")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func switchContext(ctx context.Context, contextName string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "config", "use-context", contextName)
	return cmd.Run()
}
