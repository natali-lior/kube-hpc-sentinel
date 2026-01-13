#!/usr/bin/env bash

# Load container images into Kind cluster
# Usage: ./load-to-kind.sh [cluster-name] [tag]

set -e

CLUSTER_NAME=${1:-hpc-sentinel-dev}
TAG=${2:-dev}
REGISTRY=${REGISTRY:-localhost:5001}
PROJECT="kube-hpc-sentinel"

echo "📦 Loading images to Kind cluster: ${CLUSTER_NAME}"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Check if cluster exists
if ! kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
    echo -e "${YELLOW}⚠️  Cluster '${CLUSTER_NAME}' not found${NC}"
    echo "Available clusters:"
    kind get clusters
    exit 1
fi

load_image() {
    local component=$1
    local image_name="${REGISTRY}/${PROJECT}-${component}:${TAG}"

    echo -e "${BLUE}Loading ${component}...${NC}"
    kind load docker-image "${image_name}" --name "${CLUSTER_NAME}"
    echo -e "${GREEN}✓ Loaded ${component}${NC}\n"
}

# Load all images
load_image "manager"
load_image "provisioner"
load_image "dcgm-mock-exporter"
load_image "scenario-manager"

echo -e "${GREEN}✅ All images loaded to Kind cluster!${NC}"
echo ""
echo "Verify with:"
echo "  kubectl get nodes --context kind-${CLUSTER_NAME}"
echo "  docker exec -it ${CLUSTER_NAME}-control-plane crictl images | grep ${PROJECT}"
