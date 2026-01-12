package gpu

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	devicePluginNamespace = "kube-system"
	gpuOperatorNamespace  = "gpu-operator"
)

// InstallFakeGPUOperator installs the NVIDIA k8s-device-plugin in fake mode
func InstallFakeGPUOperator(ctx context.Context, verbose bool) error {
	// Create namespace
	if err := createNamespace(ctx, gpuOperatorNamespace); err != nil {
		return err
	}

	// Apply fake GPU device plugin DaemonSet
	manifest := fakeGPUDevicePluginManifest()
	if err := applyManifest(ctx, manifest, verbose); err != nil {
		return fmt.Errorf("failed to apply device plugin manifest: %w", err)
	}

	fmt.Println("   ⏳ Waiting for device plugin pods to be ready...")
	if err := waitForDaemonSet(ctx, gpuOperatorNamespace, "nvidia-device-plugin-daemonset", 60*time.Second); err != nil {
		return err
	}

	fmt.Println("   ✓ NVIDIA fake GPU device plugin deployed")
	return nil
}

// DeployDCGMExporter deploys DCGM exporter on GPU nodes
func DeployDCGMExporter(ctx context.Context, verbose bool) error {
	manifest := dcgmExporterManifest()
	if err := applyManifest(ctx, manifest, verbose); err != nil {
		return fmt.Errorf("failed to apply DCGM exporter: %w", err)
	}

	fmt.Println("   ⏳ Waiting for DCGM exporter pods to be ready...")
	if err := waitForDaemonSet(ctx, gpuOperatorNamespace, "dcgm-exporter", 60*time.Second); err != nil {
		return err
	}

	fmt.Println("   ✓ DCGM exporter deployed")
	return nil
}

// ShowGPUNodes lists GPU nodes with their resources
func ShowGPUNodes(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "kubectl", "get", "nodes",
		"-l", "nvidia.com/gpu.present=true",
		"-o", "custom-columns=NAME:.metadata.name,GPU-COUNT:.metadata.labels.nvidia\\.com/gpu\\.count,GPU-PRODUCT:.metadata.labels.nvidia\\.com/gpu\\.product,STATUS:.status.conditions[?(@.type=='Ready')].status")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ListGPUResources shows GPU resources across the cluster
func ListGPUResources(ctx context.Context, verbose bool) error {
	fmt.Println("📊 GPU Resources per Node:\n")

	cmd := exec.CommandContext(ctx, "kubectl", "get", "nodes",
		"-l", "nvidia.com/gpu.present=true",
		"-o", "custom-columns=NAME:.metadata.name,ALLOCATABLE:.status.allocatable")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	fmt.Println("\n📦 GPU Pods:")
	cmd = exec.CommandContext(ctx, "kubectl", "get", "pods", "--all-namespaces",
		"-o", "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,NODE:.spec.nodeName,GPU-LIMIT:.spec.containers[*].resources.limits.nvidia\\.com/gpu")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// AddGPUToNode adds GPU capability to an existing node
func AddGPUToNode(ctx context.Context, nodeName string) error {
	labels := map[string]string{
		"nvidia.com/gpu.present": "true",
		"nvidia.com/gpu.count":   "8",
		"nvidia.com/gpu.product": "NVIDIA-A100-SXM4-80GB",
		"node-type":              "gpu-worker",
	}

	for key, value := range labels {
		cmd := exec.CommandContext(ctx, "kubectl", "label", "node", nodeName,
			fmt.Sprintf("%s=%s", key, value), "--overwrite")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to label node: %w", err)
		}
	}

	// Taint the node
	cmd := exec.CommandContext(ctx, "kubectl", "taint", "node", nodeName,
		"nvidia.com/gpu=present:NoSchedule", "--overwrite")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to taint node: %w", err)
	}

	fmt.Printf("✅ Node %s is now a GPU node\n", nodeName)
	return nil
}

// ShowDCGMMetrics shows sample DCGM metrics
func ShowDCGMMetrics(ctx context.Context) error {
	fmt.Println("📊 DCGM Exporter Endpoints:\n")

	cmd := exec.CommandContext(ctx, "kubectl", "get", "pods",
		"-n", gpuOperatorNamespace,
		"-l", "app=dcgm-exporter",
		"-o", "custom-columns=NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	fmt.Println("\nTo view metrics from a specific pod:")
	fmt.Println("  kubectl port-forward -n gpu-operator <pod-name> 9400:9400")
	fmt.Println("  curl http://localhost:9400/metrics")

	return nil
}

func fakeGPUDevicePluginManifest() string {
	return `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: nvidia-device-plugin-daemonset
  namespace: gpu-operator
spec:
  selector:
    matchLabels:
      name: nvidia-device-plugin
  updateStrategy:
    type: RollingUpdate
  template:
    metadata:
      labels:
        name: nvidia-device-plugin
    spec:
      tolerations:
      - key: nvidia.com/gpu
        operator: Exists
        effect: NoSchedule
      nodeSelector:
        nvidia.com/gpu.present: "true"
      priorityClassName: system-node-critical
      containers:
      - image: nvcr.io/nvidia/k8s-device-plugin:v0.14.5
        name: nvidia-device-plugin-ctr
        env:
        - name: PASS_DEVICE_SPECS
          value: "true"
        - name: FAIL_ON_INIT_ERROR
          value: "false"
        - name: DEVICE_LIST_STRATEGY
          value: "envvar"
        - name: DEVICE_ID_STRATEGY
          value: "uuid"
        - name: NVIDIA_VISIBLE_DEVICES
          value: "all"
        - name: NVIDIA_DRIVER_CAPABILITIES
          value: "compute,utility"
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop: ["ALL"]
        volumeMounts:
        - name: device-plugin
          mountPath: /var/lib/kubelet/device-plugins
      volumes:
      - name: device-plugin
        hostPath:
          path: /var/lib/kubelet/device-plugins
`
}

func dcgmExporterManifest() string {
	return `apiVersion: v1
kind: ServiceAccount
metadata:
  name: dcgm-exporter
  namespace: gpu-operator
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: dcgm-exporter
  namespace: gpu-operator
  labels:
    app: dcgm-exporter
spec:
  selector:
    matchLabels:
      app: dcgm-exporter
  updateStrategy:
    type: RollingUpdate
  template:
    metadata:
      labels:
        app: dcgm-exporter
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9400"
    spec:
      serviceAccountName: dcgm-exporter
      tolerations:
      - key: nvidia.com/gpu
        operator: Exists
        effect: NoSchedule
      nodeSelector:
        nvidia.com/gpu.present: "true"
      containers:
      - name: dcgm-exporter
        image: nvcr.io/nvidia/k8s/dcgm-exporter:3.3.0-3.2.0-ubuntu22.04
        env:
        - name: DCGM_EXPORTER_LISTEN
          value: ":9400"
        - name: DCGM_EXPORTER_KUBERNETES
          value: "true"
        - name: DCGM_EXPORTER_NO_HOSTNAME
          value: "true"
        ports:
        - name: metrics
          containerPort: 9400
        securityContext:
          runAsNonRoot: false
          runAsUser: 0
          capabilities:
            add:
            - SYS_ADMIN
        volumeMounts:
        - name: pod-resources
          mountPath: /var/lib/kubelet/pod-resources
          readOnly: true
      volumes:
      - name: pod-resources
        hostPath:
          path: /var/lib/kubelet/pod-resources
---
apiVersion: v1
kind: Service
metadata:
  name: dcgm-exporter
  namespace: gpu-operator
  labels:
    app: dcgm-exporter
spec:
  type: ClusterIP
  ports:
  - name: metrics
    port: 9400
    targetPort: 9400
    protocol: TCP
  selector:
    app: dcgm-exporter
`
}

func createNamespace(ctx context.Context, namespace string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "create", "namespace", namespace)
	if err := cmd.Run(); err != nil {
		// Namespace might already exist, check
		checkCmd := exec.CommandContext(ctx, "kubectl", "get", "namespace", namespace)
		if checkErr := checkCmd.Run(); checkErr != nil {
			return fmt.Errorf("failed to create/verify namespace: %w", err)
		}
	}
	return nil
}

func applyManifest(ctx context.Context, manifest string, verbose bool) error {
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	if verbose {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func waitForDaemonSet(ctx context.Context, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(ctx, "kubectl", "get", "daemonset", name,
			"-n", namespace,
			"-o", "jsonpath={.status.numberReady}/{.status.desiredNumberScheduled}")
		output, err := cmd.Output()
		if err == nil {
			status := string(output)
			if strings.Contains(status, "/") {
				parts := strings.Split(status, "/")
				if len(parts) == 2 && parts[0] == parts[1] && parts[0] != "0" {
					return nil
				}
			}
		}

		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timeout waiting for daemonset %s/%s", namespace, name)
}
