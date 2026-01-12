package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/natali-lior/kube-hpc-sentinel/tools/localenv/pkg/cluster"
	"github.com/natali-lior/kube-hpc-sentinel/tools/localenv/pkg/gpu"
	"github.com/natali-lior/kube-hpc-sentinel/tools/localenv/pkg/observability"
)

const (
	clusterName = "hpc-sentinel-dev"
)

var (
	verbose bool
	rootCmd = &cobra.Command{
		Use:   "localenv",
		Short: "Local GPU HPC environment manager for kube-hpc-sentinel",
		Long: `A development environment setup tool that creates a Kind cluster
with fake GPU nodes, DCGM metrics exporters, and observability stack
(Prometheus + Grafana) for testing the HPC Sentinel operator.`,
	}
)

func main() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	rootCmd.AddCommand(upCmd())
	rootCmd.AddCommand(downCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(gpuCmd())
	rootCmd.AddCommand(metricsCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func upCmd() *cobra.Command {
	var (
		workers       int
		gpuNodes      int
		withMetrics   bool
		skipOperator  bool
	)

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Spin up the local HPC development environment",
		Long: `Creates a Kind cluster with GPU nodes, installs NVIDIA fake GPU operator,
configures DCGM exporters, and optionally sets up Prometheus/Grafana.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			fmt.Println("🚀 Spinning up local HPC environment...")
			fmt.Printf("   Cluster: %s\n", clusterName)
			fmt.Printf("   Workers: %d (GPU nodes: %d)\n", workers, gpuNodes)

			// Step 1: Create Kind cluster
			fmt.Println("\n📦 Creating Kind cluster...")
			if err := cluster.CreateCluster(ctx, clusterName, workers, verbose); err != nil {
				return fmt.Errorf("failed to create cluster: %w", err)
			}

			// Step 2: Label GPU nodes
			fmt.Println("\n🏷️  Labeling GPU nodes...")
			if err := cluster.LabelGPUNodes(ctx, clusterName, gpuNodes); err != nil {
				return fmt.Errorf("failed to label GPU nodes: %w", err)
			}

			// Step 3: Install NVIDIA fake GPU operator
			if !skipOperator {
				fmt.Println("\n🎮 Installing NVIDIA fake GPU operator...")
				if err := gpu.InstallFakeGPUOperator(ctx, verbose); err != nil {
					return fmt.Errorf("failed to install GPU operator: %w", err)
				}
			}

			// Step 4: Deploy DCGM exporters
			fmt.Println("\n📊 Deploying DCGM exporters on GPU nodes...")
			if err := gpu.DeployDCGMExporter(ctx, verbose); err != nil {
				return fmt.Errorf("failed to deploy DCGM exporter: %w", err)
			}

			// Step 5: Setup observability stack
			if withMetrics {
				fmt.Println("\n📈 Setting up Prometheus and Grafana...")
				if err := observability.SetupPrometheus(ctx, verbose); err != nil {
					return fmt.Errorf("failed to setup Prometheus: %w", err)
				}
				if err := observability.SetupGrafana(ctx, verbose); err != nil {
					return fmt.Errorf("failed to setup Grafana: %w", err)
				}
			}

			fmt.Println("\n✅ Local HPC environment is ready!")
			fmt.Println("\nNext steps:")
			fmt.Println("  • Switch context: kubectl config use-context kind-" + clusterName)
			fmt.Println("  • View GPU nodes: kubectl get nodes -l nvidia.com/gpu.present=true")
			fmt.Println("  • Check DCGM metrics: kubectl port-forward -n gpu-operator svc/dcgm-exporter 9400:9400")
			if withMetrics {
				fmt.Println("  • Prometheus: kubectl port-forward -n monitoring svc/prometheus 9090:9090")
				fmt.Println("  • Grafana: kubectl port-forward -n monitoring svc/grafana 3000:3000")
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&workers, "workers", 3, "number of worker nodes")
	cmd.Flags().IntVar(&gpuNodes, "gpu-nodes", 2, "number of GPU nodes (must be <= workers)")
	cmd.Flags().BoolVar(&withMetrics, "with-metrics", false, "install Prometheus and Grafana")
	cmd.Flags().BoolVar(&skipOperator, "skip-operator", false, "skip GPU operator installation")

	return cmd
}

func downCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Tear down the local environment",
		Long:  "Deletes the Kind cluster and cleans up all resources.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			fmt.Printf("🗑️  Tearing down cluster: %s\n", clusterName)
			if err := cluster.DeleteCluster(ctx, clusterName); err != nil {
				return fmt.Errorf("failed to delete cluster: %w", err)
			}

			fmt.Println("✅ Environment cleaned up successfully!")
			return nil
		},
	}

	return cmd
}

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check the status of the local environment",
		Long:  "Displays cluster info, GPU nodes, and component health.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			exists, err := cluster.ClusterExists(ctx, clusterName)
			if err != nil {
				return fmt.Errorf("failed to check cluster: %w", err)
			}

			if !exists {
				fmt.Printf("❌ Cluster '%s' does not exist\n", clusterName)
				fmt.Println("\nRun 'localenv up' to create it.")
				return nil
			}

			fmt.Printf("✅ Cluster '%s' is running\n\n", clusterName)

			// Show cluster info
			if err := cluster.ShowClusterInfo(ctx, clusterName); err != nil {
				return err
			}

			// Show GPU nodes
			fmt.Println("\n📊 GPU Nodes:")
			if err := gpu.ShowGPUNodes(ctx); err != nil {
				return err
			}

			// Show observability stack
			fmt.Println("\n📈 Observability Stack:")
			if err := observability.ShowStatus(ctx); err != nil {
				return err
			}

			return nil
		},
	}

	return cmd
}

func gpuCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gpu",
		Short: "Manage GPU nodes and resources",
		Long:  "Commands for managing GPU nodes, fake GPUs, and DCGM exporters.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List GPU nodes and their resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			return gpu.ListGPUResources(ctx, verbose)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "add [node-name]",
		Short: "Add GPU capability to a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			nodeName := args[0]
			fmt.Printf("➕ Adding GPU capability to node: %s\n", nodeName)
			return gpu.AddGPUToNode(ctx, nodeName)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "metrics",
		Short: "Show DCGM metrics from GPU nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			return gpu.ShowDCGMMetrics(ctx)
		},
	})

	return cmd
}

func metricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Manage the observability stack",
		Long:  "Commands for setting up and managing Prometheus and Grafana.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Install Prometheus and Grafana",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			fmt.Println("📈 Installing Prometheus...")
			if err := observability.SetupPrometheus(ctx, verbose); err != nil {
				return err
			}

			fmt.Println("📊 Installing Grafana...")
			if err := observability.SetupGrafana(ctx, verbose); err != nil {
				return err
			}

			fmt.Println("\n✅ Observability stack installed!")
			fmt.Println("\nAccess URLs:")
			fmt.Println("  • Prometheus: http://localhost:9090 (kubectl port-forward -n monitoring svc/prometheus 9090:9090)")
			fmt.Println("  • Grafana: http://localhost:3000 (kubectl port-forward -n monitoring svc/grafana 3000:3000)")
			fmt.Println("    Default credentials: admin/admin")

			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall Prometheus and Grafana",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			return observability.Uninstall(ctx, verbose)
		},
	})

	return cmd
}
