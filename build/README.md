# Build Directory

Container images and build scripts for kube-hpc-sentinel components.

## Directory Structure

```
build/
├── manager/                    # Controller manager image
│   └── Dockerfile
├── provisioner/                # Cluster provisioner image
│   └── Dockerfile
├── dcgm-mock-exporter/         # Mock GPU metrics exporter
│   └── Dockerfile
├── scenario-manager/           # Scenario simulator
│   └── Dockerfile
└── scripts/                    # Build automation scripts
    ├── build-all.sh            # Build all images
    ├── load-to-kind.sh         # Load images to Kind cluster
    └── dev-rebuild.sh          # Quick rebuild for development
```

## Components

### Manager (`manager/`)

The main Kubernetes operator controller that reconciles HPCJob resources.

**Base**: `gcr.io/distroless/static:nonroot`
**Entrypoint**: `/manager`
**Port**: 8443 (metrics), 9443 (webhooks)

### Provisioner (`provisioner/`)

CLI tool for provisioning local test clusters with simulated GPU nodes.

**Base**: `gcr.io/distroless/static:nonroot`
**Entrypoint**: `/provisioner`

### DCGM Mock Exporter (`dcgm-mock-exporter/`)

HTTP server that serves simulated NVIDIA DCGM metrics on `/metrics` endpoint.

**Base**: `gcr.io/distroless/static:nonroot`
**Entrypoint**: `/dcgm-mock-exporter`
**Port**: 9400
**Metrics**: Prometheus format

### Scenario Manager (`scenario-manager/`)

Generates realistic GPU workload scenarios and writes metrics to ConfigMaps for consumption by the mock exporter.

**Base**: `gcr.io/distroless/static:nonroot`
**Entrypoint**: `/scenario-manager`

## Build Scripts

### Build All Images

```bash
# Build with default 'dev' tag
./build/scripts/build-all.sh

# Build with custom tag
./build/scripts/build-all.sh v0.1.0

# Build with custom registry
REGISTRY=ghcr.io/natali-lior ./build/scripts/build-all.sh v0.1.0
```

### Load to Kind Cluster

```bash
# Load to default cluster (hpc-sentinel-dev)
./build/scripts/load-to-kind.sh

# Load to custom cluster
./build/scripts/load-to-kind.sh my-cluster dev

# Load with custom registry
REGISTRY=ghcr.io/natali-lior ./build/scripts/load-to-kind.sh
```

### Quick Development Rebuild

For fast iteration during development:

```bash
# Rebuild just the manager
./build/scripts/dev-rebuild.sh manager

# Rebuild all components
./build/scripts/dev-rebuild.sh all

# Rebuild and load to custom cluster
./build/scripts/dev-rebuild.sh manager my-cluster
```

**What it does**:
1. Builds the specified component's Docker image
2. Loads it into the Kind cluster
3. Restarts the deployment (if it exists)

## Typical Workflows

### Initial Setup

```bash
# 1. Build all images
./build/scripts/build-all.sh dev

# 2. Create Kind cluster (using provisioner or manually)
cd cmd/provisioner && go run main.go

# 3. Load images to cluster
./build/scripts/load-to-kind.sh hpc-sentinel-dev dev

# 4. Deploy operator
cd ../.. && make deploy IMG=localhost:5001/kube-hpc-sentinel-manager:dev
```

### Development Loop (Manual)

```bash
# 1. Make code changes
vim internal/controller/hpcjob_controller.go

# 2. Quick rebuild and reload
./build/scripts/dev-rebuild.sh manager

# 3. Watch logs
kubectl logs -n kube-hpc-sentinel-system deployment/kube-hpc-sentinel-controller-manager -f
```

### Development Loop (Automated with Tilt)

For automatic rebuilds on file changes, use Tilt (see root `Tiltfile`):

```bash
# Start Tilt (rebuilds on changes)
tilt up

# Press spacebar to open web UI
# Edit code - Tilt automatically rebuilds and reloads
```

## Image Tags and Registry

### Default Configuration

```bash
REGISTRY=localhost:5001
PROJECT=kube-hpc-sentinel
TAG=dev
```

Images are tagged as: `${REGISTRY}/${PROJECT}-${COMPONENT}:${TAG}`

Examples:
- `localhost:5001/kube-hpc-sentinel-manager:dev`
- `localhost:5001/kube-hpc-sentinel-provisioner:dev`
- `localhost:5001/kube-hpc-sentinel-dcgm-mock-exporter:dev`
- `localhost:5001/kube-hpc-sentinel-scenario-manager:dev`

### Custom Registry (e.g., GitHub Container Registry)

```bash
# Build for GHCR
REGISTRY=ghcr.io/natali-lior ./build/scripts/build-all.sh v0.1.0

# Push to registry
docker push ghcr.io/natali-lior/kube-hpc-sentinel-manager:v0.1.0
docker push ghcr.io/natali-lior/kube-hpc-sentinel-provisioner:v0.1.0
docker push ghcr.io/natali-lior/kube-hpc-sentinel-dcgm-mock-exporter:v0.1.0
docker push ghcr.io/natali-lior/kube-hpc-sentinel-scenario-manager:v0.1.0
```

## Multi-Platform Builds

For building multi-platform images (amd64, arm64):

```bash
# Enable buildx
docker buildx create --use

# Build multi-platform manager
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -f build/manager/Dockerfile \
  -t ghcr.io/natali-lior/kube-hpc-sentinel-manager:v0.1.0 \
  --push \
  .
```

Or use the Makefile:

```bash
make docker-buildx IMG=ghcr.io/natali-lior/kube-hpc-sentinel-manager:v0.1.0
```

## Debugging

### Check Built Images

```bash
docker images | grep kube-hpc-sentinel
```

### Check Images in Kind

```bash
docker exec -it hpc-sentinel-dev-control-plane crictl images | grep kube-hpc-sentinel
```

### Build with Verbose Output

```bash
docker build -f build/manager/Dockerfile --progress=plain .
```

### Inspect Image

```bash
docker inspect localhost:5001/kube-hpc-sentinel-manager:dev
docker history localhost:5001/kube-hpc-sentinel-manager:dev
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Build and Push Images

on:
  push:
    branches: [main]
    tags: ['v*']

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v2

      - name: Log in to GHCR
        uses: docker/login-action@v2
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build and push manager
        run: |
          REGISTRY=ghcr.io/${{ github.repository_owner }} \
          ./build/scripts/build-all.sh ${{ github.ref_name }}

          docker push ghcr.io/${{ github.repository_owner }}/kube-hpc-sentinel-manager:${{ github.ref_name }}
          # ... push other images
```

## Troubleshooting

### Build Fails with "permission denied"

```bash
# Make scripts executable
chmod +x build/scripts/*.sh
```

### Kind Load Fails

```bash
# Check cluster exists
kind get clusters

# Check Docker daemon
docker ps

# Try manual load
kind load docker-image <image-name> --name <cluster-name>
```

### Image Not Found in Cluster

```bash
# Verify image was loaded
kubectl get nodes
docker exec -it <cluster>-control-plane crictl images

# Re-load image
./build/scripts/load-to-kind.sh
```

### Pods Not Using New Image

```bash
# Force restart deployment
kubectl rollout restart deployment/kube-hpc-sentinel-controller-manager -n kube-hpc-sentinel-system

# Or delete pod to recreate
kubectl delete pod -n kube-hpc-sentinel-system -l control-plane=controller-manager
```

### Image Pull Policy Issues

When using Kind, always set:
```yaml
imagePullPolicy: IfNotPresent  # or Never
```

In development, `imagePullPolicy: Always` will fail because Kind doesn't have a registry.

## Best Practices

1. **Use dev tag for local development**
   ```bash
   ./build/scripts/build-all.sh dev
   ```

2. **Version tags for releases**
   ```bash
   ./build/scripts/build-all.sh v0.1.0
   ```

3. **Always load after building** (for Kind)
   ```bash
   ./build/scripts/build-all.sh dev && ./build/scripts/load-to-kind.sh
   ```

4. **Use Tilt for rapid iteration**
   - Watches for file changes
   - Automatically rebuilds
   - Automatically reloads pods

5. **Keep images small**
   - Use multi-stage builds
   - Use distroless base images
   - Only COPY necessary files

6. **Security scanning**
   ```bash
   # Scan with trivy
   trivy image localhost:5001/kube-hpc-sentinel-manager:dev
   ```

## Related Documentation

- [../DEVELOPMENT_GUIDE.md](../DEVELOPMENT_GUIDE.md) - Full development guide
- [../Tiltfile](../Tiltfile) - Tilt configuration for auto-reload
- [../Makefile](../Makefile) - Build automation

## Questions?

See [../DEVELOPMENT_GUIDE.md](../DEVELOPMENT_GUIDE.md) or open an issue.
