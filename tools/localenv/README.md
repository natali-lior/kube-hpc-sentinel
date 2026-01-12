# Local HPC Development Environment

A Go-based tool for spinning up a complete local GPU-enabled HPC testing environment using Kind (Kubernetes IN Docker).

## Overview

This tool creates a migration-like workflow for setting up and tearing down a local development cluster with:

- **Kind cluster** with multiple worker nodes
- **Fake GPU nodes** using NVIDIA device plugin
- **DCGM metric exporters** on GPU nodes
- **Prometheus** for metrics collection
- **Grafana** for visualization

Think of it like database migrations, but for your entire Kubernetes GPU testing infrastructure.

## Prerequisites

- Docker Desktop running
- kubectl installed
- kind installed (`go install sigs.k8s.io/kind@latest`)
- Go 1.24+ (for building the tool)

## Installation

```bash
cd tools/localenv
go build -o localenv .
```

Or add to your PATH:
```bash
go install ./tools/localenv
```

## Usage

### Quick Start

```bash
# Spin up a complete environment with 3 workers, 2 GPU nodes, and observability
./localenv up --workers 3 --gpu-nodes 2 --with-metrics

# Check status
./localenv status

# Tear down
./localenv down
```

### Commands

#### `localenv up`

Creates the complete local HPC environment.

**Flags:**
- `--workers <n>` - Number of worker nodes (default: 3)
- `--gpu-nodes <n>` - Number of GPU nodes (default: 2, must be <= workers)
- `--with-metrics` - Install Prometheus and Grafana (default: false)
- `--skip-operator` - Skip GPU operator installation (default: false)
- `-v, --verbose` - Verbose output

**Example:**
```bash
# Minimal setup
./localenv up

# Full setup with observability
./localenv up --workers 5 --gpu-nodes 3 --with-metrics -v
```

**What it does:**
1. Creates a Kind cluster with specified worker nodes
2. Labels GPU nodes with:
   - `nvidia.com/gpu.present=true`
   - `nvidia.com/gpu.count=8` (fake 8 GPUs per node)
   - `nvidia.com/gpu.product=NVIDIA-A100-SXM4-80GB`
3. Taints GPU nodes so only GPU workloads schedule there
4. Installs NVIDIA k8s-device-plugin in fake mode
5. Deploys DCGM exporters on GPU nodes (port 9400)
6. (Optional) Installs Prometheus and Grafana

#### `localenv down`

Tears down the entire environment.

```bash
./localenv down
```

Deletes the Kind cluster and all resources.

#### `localenv status`

Shows the current state of the environment.

```bash
./localenv status
```

**Output:**
- Cluster existence
- Node list with GPU labels
- GPU operator pods
- DCGM exporter status
- Prometheus/Grafana status (if installed)

#### `localenv gpu`

Manage GPU nodes and resources.

**Subcommands:**

```bash
# List GPU nodes and resources
./localenv gpu list

# Add GPU capability to a specific node
./localenv gpu add <node-name>

# Show DCGM metrics endpoints
./localenv gpu metrics
```

#### `localenv metrics`

Manage the observability stack.

**Subcommands:**

```bash
# Install Prometheus and Grafana
./localenv metrics install

# Uninstall observability stack
./localenv metrics uninstall
```

## Architecture

### Components

```
┌─────────────────────────────────────────────┐
│             Kind Cluster                    │
│  ┌────────────┐  ┌──────────────────────┐  │
│  │  Control   │  │   Worker Nodes       │  │
│  │   Plane    │  │                      │  │
│  └────────────┘  │  ┌────────────────┐  │  │
│                  │  │  GPU Worker 1  │  │  │
│                  │  │  - 8x A100 80GB│  │  │
│                  │  │  - DCGM:9400   │  │  │
│                  │  └────────────────┘  │  │
│                  │  ┌────────────────┐  │  │
│                  │  │  GPU Worker 2  │  │  │
│                  │  │  - 8x A100 80GB│  │  │
│                  │  │  - DCGM:9400   │  │  │
│                  │  └────────────────┘  │  │
│                  │  ┌────────────────┐  │  │
│                  │  │  Worker 3      │  │  │
│                  │  │  (CPU only)    │  │  │
│                  │  └────────────────┘  │  │
│                  └──────────────────────┘  │
│                                             │
│  ┌──────────────────────────────────────┐  │
│  │     Observability (Optional)         │  │
│  │  ┌──────────────┐ ┌──────────────┐  │  │
│  │  │  Prometheus  │ │   Grafana    │  │  │
│  │  │   :9090      │ │    :3000     │  │  │
│  │  └──────────────┘ └──────────────┘  │  │
│  └──────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

### Fake GPU Setup

The NVIDIA k8s-device-plugin runs in "fake" mode where it doesn't require actual NVIDIA hardware. This is achieved by:

1. **Node Labels** - Manually applied labels tell Kubernetes about GPU resources
2. **Device Plugin** - Runs as a DaemonSet on GPU-labeled nodes
3. **Resource Advertising** - Plugin advertises `nvidia.com/gpu` resources to kubelet
4. **DCGM Exporter** - Provides GPU-like metrics (simulated in this environment)

### DCGM Metrics

DCGM (Data Center GPU Manager) Exporter runs on each GPU node and exposes metrics on port 9400:

**Sample metrics:**
- `DCGM_FI_DEV_GPU_UTIL` - GPU utilization (%)
- `DCGM_FI_DEV_FB_USED` - Frame buffer memory used (MB)
- `DCGM_FI_DEV_GPU_TEMP` - GPU temperature (C)
- `DCGM_FI_DEV_POWER_USAGE` - Power usage (W)
- `DCGM_FI_DEV_SM_CLOCK` - SM clock frequency (MHz)

### Prometheus Configuration

Prometheus is configured to scrape:
- DCGM exporters on GPU nodes
- Kubernetes API server
- Kubelet metrics

### Grafana Dashboards

Pre-configured with:
- GPU Utilization dashboard
- GPU Memory dashboard
- GPU Temperature dashboard

## Development Workflow

### 1. Start Environment

```bash
# Start with metrics
./localenv up --with-metrics
```

### 2. Deploy Your Operator

```bash
# Switch context
kubectl config use-context kind-hpc-sentinel-dev

# Install CRDs
cd ../..
make install

# Run operator locally
make run
```

### 3. Create Test HPCJob

```bash
# Apply sample job
kubectl apply -f config/samples/hpc_v1alpha1_hpcjob.yaml

# Watch it
kubectl get hpcjobs -w
```

### 4. Monitor Metrics

```bash
# Port forward Prometheus
kubectl port-forward -n monitoring svc/prometheus 9090:9090

# Port forward Grafana
kubectl port-forward -n monitoring svc/grafana 3000:3000

# Visit:
# - Prometheus: http://localhost:9090
# - Grafana: http://localhost:3000 (admin/admin)
```

### 5. View DCGM Metrics

```bash
# Get DCGM exporter pod
kubectl get pods -n gpu-operator -l app=dcgm-exporter

# Port forward to view metrics
kubectl port-forward -n gpu-operator <pod-name> 9400:9400

# Curl metrics
curl http://localhost:9400/metrics | grep DCGM
```

### 6. Cleanup

```bash
# When done
./localenv down
```

## Testing Your Operator

### Scenario 1: GPU Scheduling

```bash
# Create a pod requesting GPUs
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: gpu-test
spec:
  containers:
  - name: cuda
    image: nvidia/cuda:12.0-runtime
    command: ["sleep", "3600"]
    resources:
      limits:
        nvidia.com/gpu: "2"
  tolerations:
  - key: nvidia.com/gpu
    operator: Exists
    effect: NoSchedule
EOF

# Check scheduling
kubectl get pod gpu-test -o wide
kubectl describe pod gpu-test
```

### Scenario 2: HPCJob All-or-Nothing

```bash
# Create HPCJob requiring more GPUs than available
kubectl apply -f - <<EOF
apiVersion: hpc.nvidia.com/v1alpha1
kind: HPCJob
metadata:
  name: large-job
spec:
  image: nvidia/cuda:12.0-runtime
  gpuCount: 16  # More than available (2 nodes × 8 GPUs = 16 available)
  constraints:
    topology: nvlink
EOF

# Watch status - should stay Pending or schedule
kubectl get hpcjob large-job -w
kubectl describe hpcjob large-job
```

## Troubleshooting

### Cluster won't start

```bash
# Check Docker is running
docker ps

# Delete existing cluster if stale
kind delete cluster --name hpc-sentinel-dev

# Try again
./localenv up -v
```

### GPU nodes not labeled

```bash
# Check nodes
kubectl get nodes --show-labels

# Manually label
./localenv gpu add <node-name>
```

### DCGM exporter not starting

```bash
# Check DaemonSet
kubectl get daemonset -n gpu-operator

# Check pods
kubectl get pods -n gpu-operator -l app=dcgm-exporter

# View logs
kubectl logs -n gpu-operator -l app=dcgm-exporter --tail=50
```

### Metrics not showing in Prometheus

```bash
# Check Prometheus targets
kubectl port-forward -n monitoring svc/prometheus 9090:9090
# Visit http://localhost:9090/targets

# Check DCGM service discovery
kubectl get pods -n gpu-operator -l app=dcgm-exporter -o wide
```

## Integration with CI/CD

### GitHub Actions Example

```yaml
name: Test with Local GPU Environment

on: [push, pull_request]

jobs:
  test-gpu:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Install Kind
        run: |
          go install sigs.k8s.io/kind@latest

      - name: Build localenv tool
        run: |
          cd tools/localenv
          go build -o localenv .

      - name: Start GPU environment
        run: |
          ./tools/localenv/localenv up --workers 2 --gpu-nodes 1

      - name: Run e2e tests
        run: |
          make test-e2e

      - name: Cleanup
        if: always()
        run: |
          ./tools/localenv/localenv down
```

## Advanced Usage

### Custom GPU Configuration

Edit `pkg/cluster/kind.go` to customize:
- GPU count per node
- GPU model names
- Node taints and tolerations

### Custom DCGM Metrics

Edit `pkg/gpu/fake_operator.go` to:
- Change DCGM exporter version
- Add custom metric configurations
- Modify scrape intervals

### Custom Grafana Dashboards

Edit `pkg/observability/prometheus.go` in `grafanaDashboardConfigMap()` to add more dashboards.

## File Structure

```
tools/localenv/
├── main.go                         # CLI entry point
├── go.mod                          # Go module
├── README.md                       # This file
│
├── pkg/
│   ├── cluster/
│   │   └── kind.go                # Kind cluster management
│   ├── gpu/
│   │   └── fake_operator.go       # GPU operator and DCGM setup
│   └── observability/
│       └── prometheus.go          # Prometheus and Grafana
│
└── config/                        # (Future) YAML configs for customization
    ├── kind-config.yaml
    ├── gpu-config.yaml
    └── prometheus-config.yaml
```

## Contributing

To extend this tool:

1. Add new package in `pkg/`
2. Implement interface methods
3. Call from appropriate command in `main.go`
4. Update README with new functionality

## License

Same as kube-hpc-sentinel project (Apache 2.0)

---

## Next Steps

After setting up your local environment:

1. ✅ Read the main [DEVELOPMENT_GUIDE.md](../../DEVELOPMENT_GUIDE.md)
2. ✅ Implement your controller logic in `internal/controller/`
3. ✅ Test with `make run` against this local cluster
4. ✅ Add unit tests
5. ✅ Add e2e tests that use this environment
6. ✅ Build and deploy your operator: `make deploy`

Happy coding!
