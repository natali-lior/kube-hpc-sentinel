# Copilot Instructions for kube-hpc-sentinel

## Project Overview

**kube-hpc-sentinel** is a Kubernetes operator (controller) that schedules and manages GPU-intensive HPC (High-Performance Computing) jobs on GPU-enabled Kubernetes clusters. It uses Kubebuilder framework with controller-runtime and monitors GPU health via Prometheus metrics.

### Core Architecture

**Key Components:**
- **HPCJob CRD** (`api/v1alpha1/hpcjob_types.go`): Custom Resource defining GPU job specifications (image, GPU count, topology constraints)
- **HPCJobReconciler** (`internal/controller/hpcjob_controller.go`): Main reconciliation loop implementing state machine (Pending → Running → Complete/Failed)
- **GPU Node Discovery** (`internal/controller/gpu_node_discovery.go`): Identifies healthy GPU nodes by querying Prometheus metrics from DCGM exporters
- **Kubernetes Client** (`pkg/kube/client.go`): Abstracts K8s API interactions with filtering for GPU nodes (label: `nvidia.com/gpu.count`)
- **Prometheus Client** (`pkg/prometheus/client.go`): Queries DCGM metrics (temperature, memory, ECC errors, NVLink bandwidth) to assess node health

**Data Flow:** HPCJob → Reconciler → GPU Node Discovery (queries Prometheus) → Scheduler selects best node → Pod creation

### NVLink and GPU Topology

- Project explicitly supports **NVLink** topology for multi-GPU communication (see `NVLINK_ENHANCED_NODES`: hopper, ampere families)
- GPU node labels: `nvidia.com/gpu.clique` (multi-node NVLink), `nvidia.com/gpu.family` (GPU architecture)
- Constraints in HPCJob.Spec allow users to request specific topologies

## Development Workflow

### Build & Test Commands

```bash
# Generate CRD manifests and code (deepcopy, RBAC rules)
make manifests generate

# Run unit tests with coverage
make test  # Runs Ginkgo tests, excludes e2e

# Run e2e tests (requires Kind cluster)
make setup-test-e2e  # Create Kind cluster
make test-e2e        # Run Ginkgo-based e2e tests
make cleanup-test-e2e

# Linting
make lint            # golangci-lint
make lint-fix        # Auto-fix style issues

# Docker build & deploy
make docker-build docker-push IMG=<registry>/kube-hpc-sentinel:tag
make install         # Install CRDs to cluster
make deploy IMG=<registry>/kube-hpc-sentinel:tag
```

### Test Framework: Ginkgo v2 + Gomega

All tests use **Ginkgo/Gomega** BDD-style assertions. Control-plane tests use `envtest` (embedded API server).
- Unit tests: `internal/controller/*_test.go`, `test/utils/`
- E2e tests: `test/e2e/` (marked with `//go:build e2e` tag)
- Test suite setup: `suite_test.go` initializes test environment with controller-runtime's `envtest`

## Code Patterns & Conventions

### Logging: Zerolog with Context

Uses **rs/zerolog** (structured, JSON-capable logger), NOT standard library logging.

```go
// Pattern: Log with context and fields
log.Ctx(ctx).Warn().Msgf("gpu node does not export metrics [%v]", nodeName)
log.With().Str("job_name", name).Str("namespace", ns).Logger() // Chain context
```

**Important:** Always use `log.Ctx(ctx)` to preserve context chain; initialize with `logger.Init(level)` at startup.

### Error Handling & Reconciliation

Reconciler returns `(ctrl.Result, error)`. Follow these conventions:
- **Non-fatal errors** (e.g., missing node metrics): log & continue (return `nil` error)
- **Fatal errors** (e.g., failed to fetch HPCJob): return error to trigger requeue
- Requeue pattern: `return ctrl.Result{RequeueAfter: duration}, nil`

### Config Management

Configuration via environment variables (see `pkg/config/config.go`):
- `PORT`: Server port (default: 8080)
- `LOG_LEVEL`: Zerolog level (default: info)
- `KUBECONFIG`: Kubernetes config path

Load with `config.Load()` at startup; pass to components needing cluster info.

### Prometheus Metrics Integration

**MetricsProvider** wraps Prometheus client-go API. Key metrics (from DCGM exporter):
- `dcgm_gpu_temp`, `dcgm_gpu_util`, `dcgm_fb_usage_gpu`, `dcgm_mem_copy_util`
- `dcgm_ecc_sbe_aggregate_total`, `dcgm_power_usage`, `dcgm_nvlink_bandwidth_total`

**Health ranges** defined in `HealthMetrics` map; GPU is "unhealthy" if metrics fall outside ranges.

## Project-Specific Patterns

### HPCJob State Machine

Three phases with specific handler methods in reconciler:
1. **Pending**: Initial state; `handlePending()` selects suitable GPU node, creates Pod
2. **Running**: Pod executing; `handleRunning()` monitors health via metrics
3. **Complete/Failed**: Terminal states; `handleFailed()` cleans up resources

Update `hpcJob.Status.Phase` and `.Status.Conditions` (Kubernetes standard) to track transitions.

### GPU Node Selection

**Function:** `ListGPUNodes()` filters cluster nodes:
1. Query all nodes with `gpu.count` label via `KubeClient.GetClusterGPUNodes()`
2. Cross-reference against Prometheus health scan via `PromClient.GetFullGPUClusterHealthCheck()`
3. Skip nodes missing metrics (likely down or misconfigured DCGM exporter)

**Output:** Slice of healthy `corev1.Node` objects ready for scheduling.

### Kubernetes API Conventions

- Use **controller-runtime client** (`sigs.k8s.io/controller-runtime/pkg/client`) for CRUD; it auto-handles watches/cache
- Reconciler receives `ctrl.Request` (namespace + name); fetch resource with `r.Get(ctx, req.NamespacedName, &obj)`
- RBAC rules: Use kubebuilder markers (`// +kubebuilder:rbac:groups=...`) to auto-generate ClusterRoles

### Provisioner Pattern

`internal/provisioner/` provides pluggable cluster setup (Kind cluster provisioning, addon installation). Implements `ClusterProvider` interface:
- `CheckSystemRequirements()`: Verify docker, kubectl, etc.
- `Provision()`: Create cluster
- `InstallAddons()`: Deploy DCGM exporter, Prometheus, Grafana

Used by e2e test setup; not part of main operator.

## Integration Points

### External Dependencies

- **Kubernetes API**: via `k8s.io/client-go` (in-cluster or kubeconfig)
- **Prometheus**: Query metrics from `dcgm-mock-exporter` (default service label: `dcgm-mock-exporter`)
- **DCGM Exporter**: Expected to export metrics for each GPU on the cluster (deployed via provisioner)

### Config & Manifests

- **CRDs**: `config/crd/bases/hpc.nvidia.com_hpcjobs.yaml` (auto-generated from `hpcjob_types.go`)
- **RBAC**: `config/rbac/` (roles, service account, bindings; auto-generated from kubebuilder markers)
- **Deployment**: `config/manager/manager.yaml` (pod spec, environment)
- **Prometheus ServiceMonitor**: `config/prometheus/monitor.yaml` (scrapes operator metrics)

## Quick Start for Contributors

1. **Modify HPCJob CRD?** → Edit `api/v1alpha1/hpcjob_types.go` → Run `make manifests generate`
2. **Add reconciliation logic?** → Edit `internal/controller/hpcjob_controller.go` or create new utility file
3. **Test GPU discovery logic?** → Check `gpu_node_discovery.go` and `internal/controller/hpcjob_controller_test.go` for mocking patterns
4. **Debug in cluster?** → Deploy with `make deploy IMG=...` → Check logs with `kubectl logs -f deployment/hpc-sentinel-controller-manager`
5. **Run locally?** → `go run ./cmd/manager/main.go` (requires kubeconfig + Prometheus access)

**Tip:** When querying Prometheus, use provided `GpuClusterHealthScanPrmQuery` label selector to ensure correct metric filtering.
