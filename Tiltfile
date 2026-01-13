# Tiltfile for kube-hpc-sentinel development
# Provides fast, automated rebuild-and-reload workflow

# Configuration
CLUSTER_NAME = 'hpc-sentinel-dev'
NAMESPACE = 'kube-hpc-sentinel-system'
REGISTRY = 'localhost:5001'
PROJECT = 'kube-hpc-sentinel'

# Allow environment overrides
cluster_name = os.getenv('CLUSTER_NAME', CLUSTER_NAME)
registry = os.getenv('REGISTRY', REGISTRY)

# Set default Kubernetes context
allow_k8s_contexts('kind-' + cluster_name)

; # Install CRDs first
; k8s_yaml(kustomize('config/crd'))

; # Build and deploy manager (controller)
; docker_build(
;     registry + '/' + PROJECT + '-manager',
;     '.',
;     dockerfile='build/manager/Dockerfile',
;     only=[
;         './go.mod',
;         './go.sum',
;         './cmd/manager',
;         './api',
;         './internal/controller',
;         './pkg',
;     ],
;     live_update=[
;         sync('./cmd/manager', '/workspace/cmd/manager'),
;         sync('./internal/controller', '/workspace/internal/controller'),
;         sync('./api', '/workspace/api'),
;         sync('./pkg', '/workspace/pkg'),
;         run('cd /workspace && go build -o manager cmd/manager/main.go'),
;         restart_container(),
;     ],
; )

; # Deploy manager using kustomize
; k8s_yaml(kustomize('config/default'))
; k8s_resource(
;     'kube-hpc-sentinel-controller-manager',
;     port_forwards=['8443:8443'],  # Metrics endpoint
;     labels=['operator'],
; )

# Build DCGM mock exporter
docker_build(
    registry + '/' + PROJECT + '-dcgm-mock-exporter',
    '.',
    dockerfile='build/dcgm-mock-exporter/Dockerfile',
    only=[
        './go.mod',
        './go.sum',
        './cmd/dcgm-mock-exporter',
        './internal/provisioner/fake-scenario',
    ],
    live_update=[
        sync('./cmd/dcgm-mock-exporter', '/workspace/cmd/dcgm-mock-exporter'),
        sync('./internal/provisioner/fake-scenario', '/workspace/internal/provisioner/fake-scenario'),
        run('cd /workspace && go build -o dcgm-mock-exporter cmd/dcgm-mock-exporter/main.go'),
        restart_container(),
    ],
)

# Deploy DCGM exporter (if manifests exist in provisioner)
# Uncomment if you extract these to config/
# k8s_yaml('./config/observability/dcgm-exporter.yaml')
# k8s_resource(
#     'dcgm-mock-exporter',
#     port_forwards=['9400:9400'],  # Metrics endpoint
#     labels=['observability'],
# )

# Build scenario manager
docker_build(
    registry + '/' + PROJECT + '-scenario-manager',
    '.',
    dockerfile='build/scenario-manager/Dockerfile',
    only=[
        './go.mod',
        './go.sum',
        './cmd/fake-scenario-manager',
        './internal/provisioner/fake-scenario',
        './pkg',
    ],
    live_update=[
        sync('./cmd/fake-scenario-manager', '/workspace/cmd/fake-scenario-manager'),
        sync('./internal/provisioner/fake-scenario', '/workspace/internal/provisioner/fake-scenario'),
        run('cd /workspace && go build -o scenario-manager cmd/fake-scenario-manager/main.go'),
        restart_container(),
    ],
)

; # Local resource for running tests
; local_resource(
;     'test',
;     'make test',
;     deps=['./api', './internal', './pkg'],
;     labels=['test'],
;     auto_init=False,  # Only run when triggered
;     trigger_mode=TRIGGER_MODE_MANUAL,
; )

; # Local resource for running e2e tests
; local_resource(
;     'test-e2e',
;     'make test-e2e',
;     resource_deps=['kube-hpc-sentinel-controller-manager'],
;     labels=['test'],
;     auto_init=False,
;     trigger_mode=TRIGGER_MODE_MANUAL,
; )

# Local resource for linting
local_resource(
    'lint',
    'make lint',
    deps=['./api', './internal', './pkg', './cmd'],
    labels=['quality'],
    auto_init=False,
    trigger_mode=TRIGGER_MODE_MANUAL,
)

; # Local resource for generating manifests
; local_resource(
;     'generate-manifests',
;     'make generate && make manifests',
;     deps=['./api/v1alpha1/hpcjob_types.go', './internal/controller/hpcjob_controller.go'],
;     labels=['tools'],
;     auto_init=False,
;     trigger_mode=TRIGGER_MODE_MANUAL,
; )

# Helpful message
print("""
╔════════════════════════════════════════════════════════════════╗
║                  Kube HPC Sentinel - Tilt                      ║
╠════════════════════════════════════════════════════════════════╣
║  Press SPACE to open Tilt UI                                   ║
║                                                                ║
║  Resources:                                                    ║
║    • kube-hpc-sentinel-controller-manager (port 8443)          ║
║                                                                ║
║  Manual Triggers:                                              ║
║    • test                     - Run unit tests                 ║
║    • test-e2e                 - Run e2e tests                  ║
║    • lint                     - Run linter                     ║
║    • generate-manifests       - Regenerate CRDs/RBAC           ║
║                                                                ║
║  Quick Actions:                                                ║
║    • Edit code                - Auto rebuilds and reloads      ║
║    • kubectl logs -f          - Stream logs                    ║
║    • kubectl apply -f sample  - Test HPCJob creation           ║
║                                                                ║
║  Cluster: kind-""" + cluster_name + """
║  Registry: """ + registry + """
╚════════════════════════════════════════════════════════════════╝
""")
