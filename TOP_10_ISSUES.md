# Top 10 Issues to Fix - kube-hpc-sentinel

**Project Status**: ~45% complete - Excellent infrastructure, missing core functionality

**Last Updated**: 2026-01-13

---

## Critical Issues (P0 - Must Fix Immediately)

### 1. Empty Controller Reconciliation Loop ⚠️ BLOCKER

**Severity**: P0 (Critical)
**File**: `internal/controller/hpcjob_controller.go:49-55`
**Effort**: 3-5 days
**Impact**: Without this, the operator does absolutely nothing

**Current State**:
```go
func (r *HPCJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    _ = logf.FromContext(ctx)

    // TODO(user): your logic here  ← EMPTY!

    return ctrl.Result{}, nil
}
```

**What Needs to Be Done**:
1. Fetch HPCJob resource from cluster
2. List all GPU nodes in cluster
3. Filter nodes by topology constraints (nvlink)
4. Check if enough GPUs are available (all-or-nothing)
5. If insufficient resources:
   - Set Phase = "Pending"
   - Add condition "WaitingForResources"
   - Requeue after 30 seconds
6. If resources available:
   - Create Pod/Job with GPU resource requests
   - Set node affinity for selected GPU nodes
   - Add owner reference for garbage collection
   - Set Phase = "Running"
7. Watch for Pod completion
8. Update status conditions
9. Implement finalizer for cleanup

**Implementation Template**:
```go
func (r *HPCJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := logf.FromContext(ctx)

    // 1. Fetch HPCJob
    var hpcJob hpcv1alpha1.HPCJob
    if err := r.Get(ctx, req.NamespacedName, &hpcJob); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 2. Find GPU nodes
    gpuNodes, err := r.findGPUNodes(ctx, &hpcJob)
    if err != nil {
        return ctrl.Result{}, err
    }

    // 3. Check availability (all-or-nothing)
    if !r.hasEnoughGPUs(gpuNodes, hpcJob.Spec.GPUCount) {
        return r.updateStatusPending(ctx, &hpcJob, "InsufficientGPUs")
    }

    // 4. Create Pod with GPU requests
    pod := r.buildGPUPod(&hpcJob, gpuNodes[0])
    if err := r.Create(ctx, pod); err != nil {
        if !errors.IsAlreadyExists(err) {
            return ctrl.Result{}, err
        }
    }

    // 5. Update status
    return r.updateStatusRunning(ctx, &hpcJob, pod.Name)
}
```

**Files to Create/Modify**:
- `internal/controller/hpcjob_controller.go` - Main implementation
- `internal/controller/gpu_node_finder.go` - GPU discovery logic (new file)
- `internal/controller/pod_builder.go` - Pod generation (new file)
- `internal/controller/status_updater.go` - Status helpers (new file)

**Testing Requirements**:
- Unit tests for each helper function
- Integration test with fake GPU nodes
- E2E test with Kind cluster + mock GPUs

---

### 2. Missing RBAC Permissions ⚠️ SECURITY RISK

**Severity**: P0 (Critical)
**File**: `internal/controller/hpcjob_controller.go` (needs markers), `config/rbac/role.yaml`
**Effort**: 30 minutes
**Impact**: Controller cannot list nodes or create pods - will fail with permission errors

**Current RBAC** (config/rbac/role.yaml):
```yaml
rules:
- apiGroups: [""]
  resources: [pods]
  verbs: [get, list, watch]  # Missing: create, update, delete
- apiGroups: [hpc.nvidia.com]
  resources: [hpcjobs, hpcjobs/status, hpcjobs/finalizers]
  verbs: [get, list, watch, create, update, patch, delete]
```

**Missing Permissions**:
- Cannot list Nodes (needed for GPU discovery)
- Cannot create Pods (needed for workload execution)
- Cannot create/manage Jobs (if using batch/v1 Jobs)
- Cannot update Pod status (if watching pods)

**Fix**:

Add to `internal/controller/hpcjob_controller.go`:
```go
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get;update;patch
```

Then run:
```bash
make manifests  # Regenerates config/rbac/role.yaml
```

**Verification**:
```bash
# After deploying
kubectl auth can-i list nodes --as=system:serviceaccount:kube-hpc-sentinel-system:kube-hpc-sentinel-controller-manager
kubectl auth can-i create pods --as=system:serviceaccount:kube-hpc-sentinel-system:kube-hpc-sentinel-controller-manager
```

---

## High Priority Issues (P1 - Fix Soon)

### 3. Incorrect Dockerfile Build Path

**Severity**: P1 (High)
**File**: `Dockerfile:22`
**Effort**: 2 minutes
**Impact**: Build might fail in some environments

**Current**:
```dockerfile
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -a -o manager cmd/main.go
```

**Problem**: `cmd/main.go` doesn't exist, correct path is `cmd/manager/main.go`

**Fix**:
```dockerfile
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -a -o manager cmd/manager/main.go
```

**Why This Works Now**: The build actually succeeds because Go's module system resolves the package, but it's technically incorrect and may cause issues with some build tools.

---

### 4. Makefile Missing Additional Binaries

**Severity**: P1 (High)
**File**: `Makefile`
**Effort**: 30 minutes
**Impact**: Can't easily build provisioner, mock-exporter, scenario-manager

**Current State**: Makefile only builds manager binary

**Add These Targets**:
```makefile
##@ Build Additional Tools

.PHONY: build-all
build-all: build build-provisioner build-exporter build-scenario-manager ## Build all binaries

.PHONY: build-provisioner
build-provisioner: ## Build the cluster provisioner
	go build -o bin/provisioner cmd/provisioner/main.go

.PHONY: build-exporter
build-exporter: ## Build the DCGM mock exporter
	go build -o bin/dcgm-mock-exporter cmd/dcgm-mock-exporter/main.go

.PHONY: build-scenario-manager
build-scenario-manager: ## Build the fake scenario manager
	go build -o bin/scenario-manager cmd/fake-scenario-manager/main.go

##@ Docker Images

.PHONY: docker-build-all
docker-build-all: docker-build docker-build-provisioner docker-build-exporter docker-build-scenario-manager ## Build all container images

.PHONY: docker-build-provisioner
docker-build-provisioner: ## Build provisioner image
	docker build -t ${IMG_PROVISIONER} -f build/provisioner/Dockerfile .

.PHONY: docker-build-exporter
docker-build-exporter: ## Build exporter image
	docker build -t ${IMG_EXPORTER} -f build/exporter/Dockerfile .

.PHONY: docker-build-scenario-manager
docker-build-scenario-manager: ## Build scenario manager image
	docker build -t ${IMG_SCENARIO_MGR} -f build/scenario-manager/Dockerfile .
```

**Add Variables** (top of Makefile):
```makefile
IMAGE_TAG_BASE ?= ghcr.io/natali-lior/kube-hpc-sentinel
IMG_PROVISIONER ?= $(IMAGE_TAG_BASE)-provisioner:$(VERSION)
IMG_EXPORTER ?= $(IMAGE_TAG_BASE)-dcgm-mock-exporter:$(VERSION)
IMG_SCENARIO_MGR ?= $(IMAGE_TAG_BASE)-scenario-manager:$(VERSION)
```

---

### 5. Remove All TODO(user) Comments

**Severity**: P1 (High)
**File**: Multiple files
**Effort**: 1-2 hours
**Impact**: Looks unprofessional, confuses contributors

**Files with TODOs**:
- `README.md` (4 occurrences)
- `internal/controller/hpcjob_controller.go` (3 occurrences)
- `internal/controller/hpcjob_controller_test.go` (2 occurrences)
- `test/e2e/e2e_test.go` (2 occurrences)

**Action Plan**:
1. Review each TODO
2. Either implement the functionality or delete if not needed
3. Replace generic TODOs with specific GitHub issues

**Example Replacements**:
```go
// Before:
// TODO(user): Add more specific assertions

// After (Option 1 - Implement):
It("Should create a pod with GPU resources", func() {
    Eventually(func() int {
        var pods corev1.PodList
        k8sClient.List(ctx, &pods)
        return len(pods.Items)
    }).Should(BeNumerically(">", 0))
})

// After (Option 2 - Issue Reference):
// TODO(#42): Add validation for topology constraints
```

---

### 6. Write Real Controller Unit Tests

**Severity**: P1 (High)
**File**: `internal/controller/hpcjob_controller_test.go`
**Effort**: 2-3 days
**Impact**: Can't verify controller logic works

**Current Test**:
```go
It("should successfully reconcile the resource", func() {
    // TODO(user): Add more specific assertions
})
```

**Required Test Cases**:
```go
Context("When GPU resources are available", func() {
    It("Should create a pod with GPU requests", func() {...})
    It("Should set status phase to Running", func() {...})
    It("Should add owner reference to pod", func() {...})
})

Context("When GPU resources are insufficient", func() {
    It("Should set status phase to Pending", func() {...})
    It("Should add WaitingForResources condition", func() {...})
    It("Should requeue reconciliation", func() {...})
})

Context("When topology constraint is nvlink", func() {
    It("Should only select nvlink-capable nodes", func() {...})
    It("Should reject nodes without nvlink", func() {...})
})

Context("When pod completes", func() {
    It("Should set status phase to Complete", func() {...})
    It("Should not requeue reconciliation", func() {...})
})

Context("When pod fails", func() {
    It("Should set status phase to Failed", func() {...})
    It("Should add error condition", func() {...})
})

Context("When HPCJob is deleted", func() {
    It("Should clean up owned pods", func() {...})
    It("Should remove finalizer", func() {...})
})
```

**Test Setup Example**:
```go
BeforeEach(func() {
    // Create fake GPU nodes
    node1 := &corev1.Node{
        ObjectMeta: metav1.ObjectMeta{
            Name: "gpu-node-1",
            Labels: map[string]string{
                "nvidia.com/gpu.present": "true",
                "nvidia.com/gpu.count":   "8",
            },
        },
    }
    Expect(k8sClient.Create(ctx, node1)).To(Succeed())
})
```

---

## Medium Priority Issues (P2 - Should Fix)

### 7. Documentation Inconsistency: HPCWorkflow vs HPCJob

**Severity**: P2 (Medium)
**File**: `DEVELOPMENT_GUIDE.md`, `README.md`
**Effort**: 2-3 hours
**Impact**: Confuses users and contributors

**Problem**: Development guide extensively discusses "HPCWorkflow" (multi-step pipelines) but actual CRD is "HPCJob" (single job execution). These are different concepts.

**Decision Needed**:

**Option A**: Rename HPCJob → HPCWorkflow
- More features (multi-step)
- Aligns with guide
- More complex implementation

**Option B**: Update guide to use HPCJob (RECOMMENDED)
- Simpler, focused scope
- Match existing implementation
- Clearer purpose

**Option C**: Keep both
- HPCJob: Single GPU job
- HPCWorkflow: Multi-step pipeline (future)
- Document the relationship

**Recommended Action** (Option B):
```bash
# Update guide
sed -i 's/HPCWorkflow/HPCJob/g' DEVELOPMENT_GUIDE.md

# Keep a note about future plans
echo "## Future: HPCWorkflow CRD" >> DEVELOPMENT_GUIDE.md
echo "For multi-step DAG pipelines, we plan to add HPCWorkflow in v1beta1" >> DEVELOPMENT_GUIDE.md
```

---

### 8. Add Validation Webhook

**Severity**: P2 (Medium)
**Effort**: 4-6 hours
**Impact**: Invalid HPCJobs can be created, causing runtime errors

**What's Missing**: No validation of:
- Topology values (only "nvlink" is valid, but not enforced)
- GPU count (negative values not blocked at webhook level)
- Image validity
- Resource constraints

**Implementation**:
```bash
# Create webhook
kubebuilder create webhook \
  --group hpc \
  --version v1alpha1 \
  --kind HPCJob \
  --programmatic-validation
```

**Add to `api/v1alpha1/hpcjob_webhook.go`**:
```go
func (r *HPCJob) ValidateCreate() (admission.Warnings, error) {
    // Validate topology
    if r.Spec.Constraints.Topology != "" {
        if r.Spec.Constraints.Topology != NVLINK {
            return nil, fmt.Errorf("invalid topology: %s (supported: %s)",
                r.Spec.Constraints.Topology, NVLINK)
        }
    }

    // Validate GPU count
    if r.Spec.GPUCount < 1 {
        return nil, fmt.Errorf("gpuCount must be >= 1, got %d", r.Spec.GPUCount)
    }

    // Validate image
    if r.Spec.Image == "" {
        return nil, fmt.Errorf("image cannot be empty")
    }

    return nil, nil
}
```

**Benefits**:
- Fail fast (at admission, not reconciliation)
- Better error messages for users
- Prevents invalid state

---

### 9. Clean Up README.md

**Severity**: P2 (Medium)
**File**: `README.md`
**Effort**: 1 hour
**Impact**: First impression for contributors

**Current Issues**:
- Has `TODO(user): Add simple overview of...` markers
- Generic kubebuilder description
- No actual project description
- No examples of HPCJob usage

**Required Sections**:
```markdown
# Kube HPC Sentinel

GPU-aware Kubernetes operator for high-performance computing workloads with all-or-nothing scheduling semantics.

## Features

- 🎯 All-or-nothing GPU scheduling (entire job gets resources or waits)
- 🔗 Topology-aware placement (nvlink-aware scheduling)
- 📊 DCGM metrics integration
- 🧪 Comprehensive testing with fake GPU environments

## Quick Start

[... installation instructions ...]

## Example

```yaml
apiVersion: hpc.nvidia.com/v1alpha1
kind: HPCJob
metadata:
  name: training-job
spec:
  image: nvidia/cuda:12.0-runtime
  gpuCount: 8
  constraints:
    topology: nvlink
```

## Architecture

[... diagram ...]

## Development

See [DEVELOPMENT_GUIDE.md](./DEVELOPMENT_GUIDE.md)
```

---

### 10. Remove Kind from go.mod Dependencies

**Severity**: P3 (Low)
**File**: `go.mod:102`
**Effort**: 30 minutes
**Impact**: Inflates binary size, should be external tool

**Current**:
```go
require (
    ...
    sigs.k8s.io/kind v0.20.0 // indirect
)
```

**Problem**: Kind is used by provisioner but shouldn't be a Go module dependency. It should be an external CLI tool.

**Fix**:

1. Remove from go.mod:
```bash
go mod edit -droprequire sigs.k8s.io/kind
go mod tidy
```

2. Update provisioner to shell out to kind CLI:
```go
// Before:
import "sigs.k8s.io/kind/pkg/cluster"

// After:
cmd := exec.Command("kind", "create", "cluster", ...)
```

3. Document requirement:
```markdown
## Prerequisites

- kubectl
- kind (install: `go install sigs.k8s.io/kind@latest`)
- docker
```

**Trade-off**: Slightly more code to shell out to kind, but cleaner dependencies and smaller binaries.

---

## Summary by Priority

| Priority | Count | Estimated Effort |
|----------|-------|------------------|
| P0 (Critical) | 2 | 3-5 days |
| P1 (High) | 4 | 1-2 days |
| P2 (Medium) | 3 | 2-3 days |
| P3 (Low) | 1 | 0.5 days |
| **TOTAL** | **10** | **~7-11 days** |

## Recommended Fix Order

### Week 1: Core Functionality
1. ✅ Fix RBAC permissions (30 min)
2. ✅ Fix Dockerfile path (2 min)
3. ⚠️ Implement controller reconciliation (3-5 days)
4. ✅ Write controller unit tests (2-3 days)

### Week 2: Quality & Polish
5. ✅ Add Makefile targets for additional binaries (30 min)
6. ✅ Remove TODO comments (1-2 hours)
7. ✅ Clean up README (1 hour)
8. ✅ Fix documentation inconsistency (2-3 hours)

### Week 3: Enhancements (Optional)
9. ✅ Add validation webhook (4-6 hours)
10. ✅ Clean up Kind dependency (30 min)

## Success Metrics

After fixing these issues:

- [ ] Controller successfully schedules GPU workloads
- [ ] All tests pass with real assertions
- [ ] No TODO comments remain
- [ ] Documentation is consistent
- [ ] RBAC permissions are correct
- [ ] All binaries can be built
- [ ] README has proper project description
- [ ] Webhooks validate input

## Testing Checklist

After implementation:

```bash
# 1. Build everything
make build-all

# 2. Run tests
make test
make test-e2e

# 3. Deploy to Kind
cd cmd/provisioner
go run main.go

# 4. Install operator
make install
make deploy

# 5. Create HPCJob
kubectl apply -f config/samples/hpc_v1alpha1_hpcjob.yaml

# 6. Verify scheduling
kubectl get hpcjob hpcjob-sample -o yaml
kubectl get pods -l hpc.nvidia.com/job=hpcjob-sample

# 7. Check RBAC
kubectl auth can-i list nodes --as=system:serviceaccount:kube-hpc-sentinel-system:kube-hpc-sentinel-controller-manager
```

---

## Related Documents

- [DEVELOPMENT_GUIDE.md](./DEVELOPMENT_GUIDE.md) - Comprehensive development guide
- [cmd/provisioner/README.md](./cmd/provisioner/README.md) - Local environment setup
- [internal/controller/](./internal/controller/) - Controller implementation

## Questions?

Open an issue or discussion on GitHub.
