#!/usr/bin/env bash

# Build all container images for kube-hpc-sentinel
# Usage: ./build-all.sh [tag]

set -e

TAG=${1:-dev}
REGISTRY=${REGISTRY:-localhost:5001}
PROJECT="kube-hpc-sentinel"

echo "🏗️  Building all images with tag: ${TAG}"
echo "📦 Registry: ${REGISTRY}"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

build_image() {
    local component=$1
    local dockerfile=$2
    local image_name="${REGISTRY}/${PROJECT}-${component}:${TAG}"

    echo -e "${BLUE}Building ${component}...${NC}"
    docker build \
        -f "${dockerfile}" \
        -t "${image_name}" \
        --progress=plain \
        .

    echo -e "${GREEN}✓ Built ${image_name}${NC}\n"
}

# Change to project root
cd "$(dirname "$0")/../.."

# Build all images
build_image "manager" "build/manager/Dockerfile"
build_image "dcgm-mock-exporter" "build/dcgm-mock-exporter/Dockerfile"
build_image "scenario-manager" "build/scenario-manager/Dockerfile"

echo -e "${GREEN}✅ All images built successfully!${NC}"
echo ""
echo "Images:"
docker images | grep "${PROJECT}" | grep "${TAG}"
