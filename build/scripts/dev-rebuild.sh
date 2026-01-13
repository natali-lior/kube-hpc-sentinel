#!/usr/bin/env bash

# Quick rebuild and reload for development
# Usage: ./dev-rebuild.sh [component] [cluster-name]
#   component: manager, provisioner, dcgm-mock-exporter, scenario-manager, or 'all'
#   cluster-name: Kind cluster name (default: hpc-sentinel-dev)

set -e

COMPONENT=${1:-manager}
CLUSTER_NAME=${2:-hpc-sentinel-dev}
TAG="dev"
REGISTRY=${REGISTRY:-localhost:5001}
PROJECT="kube-hpc-sentinel"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

cd "$(dirname "$0")/../.."

rebuild_component() {
    local component=$1
    local dockerfile="build/${component}/Dockerfile"
    local image_name="${REGISTRY}/${PROJECT}-${component}:${TAG}"

    echo -e "${BLUE}🏗️  Rebuilding ${component}...${NC}"

    if [[ ! -f "${dockerfile}" ]]; then
        echo -e "${RED}❌ Dockerfile not found: ${dockerfile}${NC}"
        return 1
    fi

    # Build image
    docker build \
        -f "${dockerfile}" \
        -t "${image_name}" \
        --progress=plain \
        .

    echo -e "${GREEN}✓ Built ${image_name}${NC}"

    # Load to Kind
    echo -e "${BLUE}📦 Loading to Kind cluster: ${CLUSTER_NAME}...${NC}"
    kind load docker-image "${image_name}" --name "${CLUSTER_NAME}"

    echo -e "${GREEN}✓ Loaded to cluster${NC}"

    # Restart pods (if deployed)
    echo -e "${BLUE}🔄 Restarting pods...${NC}"
    case "${component}" in
        manager)
            kubectl rollout restart deployment/kube-hpc-sentinel-controller-manager \
                -n kube-hpc-sentinel-system 2>/dev/null || echo "Manager not deployed yet"
            ;;
        dcgm-mock-exporter)
            kubectl rollout restart deployment/dcgm-mock-exporter \
                -n monitoring 2>/dev/null || echo "DCGM exporter not deployed yet"
            ;;
        scenario-manager)
            kubectl rollout restart deployment/fake-scenario-manager \
                -n monitoring 2>/dev/null || echo "Scenario manager not deployed yet"
            ;;
    esac

    echo -e "${GREEN}✅ ${component} rebuilt and reloaded!${NC}\n"
}

if [[ "${COMPONENT}" == "all" ]]; then
    echo -e "${YELLOW}Building all components...${NC}\n"
    rebuild_component "manager"
    rebuild_component "provisioner"
    rebuild_component "dcgm-mock-exporter"
    rebuild_component "scenario-manager"
else
    rebuild_component "${COMPONENT}"
fi

echo -e "${GREEN}🎉 Done!${NC}"
echo ""
echo "Next steps:"
echo "  kubectl get pods -A --context kind-${CLUSTER_NAME}"
echo "  kubectl logs -n kube-hpc-sentinel-system deployment/kube-hpc-sentinel-controller-manager -f"
