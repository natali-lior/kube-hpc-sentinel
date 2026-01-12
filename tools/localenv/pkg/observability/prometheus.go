package observability

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	monitoringNamespace = "monitoring"
)

// SetupPrometheus installs Prometheus with GPU metrics scraping
func SetupPrometheus(ctx context.Context, verbose bool) error {
	// Create monitoring namespace
	if err := createNamespace(ctx, monitoringNamespace); err != nil {
		return err
	}

	// Apply Prometheus manifests
	manifests := []string{
		prometheusConfigMap(),
		prometheusDeployment(),
		prometheusService(),
		prometheusServiceMonitor(),
	}

	for _, manifest := range manifests {
		if err := applyManifest(ctx, manifest, verbose); err != nil {
			return err
		}
	}

	fmt.Println("   ⏳ Waiting for Prometheus to be ready...")
	if err := waitForDeployment(ctx, monitoringNamespace, "prometheus", 90*time.Second); err != nil {
		return err
	}

	fmt.Println("   ✓ Prometheus deployed")
	return nil
}

// SetupGrafana installs Grafana with pre-configured dashboards
func SetupGrafana(ctx context.Context, verbose bool) error {
	manifests := []string{
		grafanaDashboardConfigMap(),
		grafanaDatasourceConfigMap(),
		grafanaDeployment(),
		grafanaService(),
	}

	for _, manifest := range manifests {
		if err := applyManifest(ctx, manifest, verbose); err != nil {
			return err
		}
	}

	fmt.Println("   ⏳ Waiting for Grafana to be ready...")
	if err := waitForDeployment(ctx, monitoringNamespace, "grafana", 90*time.Second); err != nil {
		return err
	}

	fmt.Println("   ✓ Grafana deployed")
	return nil
}

// ShowStatus shows the status of the observability stack
func ShowStatus(ctx context.Context) error {
	// Check if monitoring namespace exists
	cmd := exec.CommandContext(ctx, "kubectl", "get", "namespace", monitoringNamespace)
	if err := cmd.Run(); err != nil {
		fmt.Println("  ❌ Monitoring stack not installed")
		return nil
	}

	fmt.Println("  Prometheus:")
	cmd = exec.CommandContext(ctx, "kubectl", "get", "deployment", "prometheus",
		"-n", monitoringNamespace,
		"-o", "custom-columns=STATUS:.status.conditions[?(@.type=='Available')].status,REPLICAS:.status.replicas")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	fmt.Println("\n  Grafana:")
	cmd = exec.CommandContext(ctx, "kubectl", "get", "deployment", "grafana",
		"-n", monitoringNamespace,
		"-o", "custom-columns=STATUS:.status.conditions[?(@.type=='Available')].status,REPLICAS:.status.replicas")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	return nil
}

// Uninstall removes the observability stack
func Uninstall(ctx context.Context, verbose bool) error {
	fmt.Println("🗑️  Removing observability stack...")
	cmd := exec.CommandContext(ctx, "kubectl", "delete", "namespace", monitoringNamespace)
	if verbose {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete monitoring namespace: %w", err)
	}

	fmt.Println("✅ Observability stack removed")
	return nil
}

func prometheusConfigMap() string {
	return `apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-config
  namespace: monitoring
data:
  prometheus.yml: |
    global:
      scrape_interval: 15s
      evaluation_interval: 15s

    scrape_configs:
      # Scrape DCGM exporters
      - job_name: 'dcgm-exporter'
        kubernetes_sd_configs:
        - role: pod
          namespaces:
            names:
            - gpu-operator
        relabel_configs:
        - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
          action: keep
          regex: true
        - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_port]
          action: replace
          target_label: __address__
          regex: ([^:]+)(?::\d+)?;(\d+)
          replacement: $1:$2
        - source_labels: [__meta_kubernetes_pod_label_app]
          action: keep
          regex: dcgm-exporter

      # Scrape Kubernetes API server
      - job_name: 'kubernetes-apiservers'
        kubernetes_sd_configs:
        - role: endpoints
        scheme: https
        tls_config:
          ca_file: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
        bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
        relabel_configs:
        - source_labels: [__meta_kubernetes_namespace, __meta_kubernetes_service_name, __meta_kubernetes_endpoint_port_name]
          action: keep
          regex: default;kubernetes;https

      # Scrape kubelet metrics
      - job_name: 'kubelet'
        kubernetes_sd_configs:
        - role: node
        scheme: https
        tls_config:
          ca_file: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
          insecure_skip_verify: true
        bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
        relabel_configs:
        - action: labelmap
          regex: __meta_kubernetes_node_label_(.+)
`
}

func prometheusDeployment() string {
	return `apiVersion: apps/v1
kind: Deployment
metadata:
  name: prometheus
  namespace: monitoring
spec:
  replicas: 1
  selector:
    matchLabels:
      app: prometheus
  template:
    metadata:
      labels:
        app: prometheus
    spec:
      serviceAccountName: prometheus
      containers:
      - name: prometheus
        image: prom/prometheus:v2.48.0
        args:
        - '--config.file=/etc/prometheus/prometheus.yml'
        - '--storage.tsdb.path=/prometheus'
        - '--web.enable-lifecycle'
        ports:
        - containerPort: 9090
          name: web
        volumeMounts:
        - name: config
          mountPath: /etc/prometheus
        - name: storage
          mountPath: /prometheus
      volumes:
      - name: config
        configMap:
          name: prometheus-config
      - name: storage
        emptyDir: {}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: prometheus
  namespace: monitoring
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: prometheus
rules:
- apiGroups: [""]
  resources:
  - nodes
  - nodes/proxy
  - services
  - endpoints
  - pods
  verbs: ["get", "list", "watch"]
- apiGroups:
  - extensions
  resources:
  - ingresses
  verbs: ["get", "list", "watch"]
- nonResourceURLs: ["/metrics"]
  verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: prometheus
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: prometheus
subjects:
- kind: ServiceAccount
  name: prometheus
  namespace: monitoring
`
}

func prometheusService() string {
	return `apiVersion: v1
kind: Service
metadata:
  name: prometheus
  namespace: monitoring
spec:
  type: ClusterIP
  ports:
  - port: 9090
    targetPort: 9090
    name: web
  selector:
    app: prometheus
`
}

func prometheusServiceMonitor() string {
	return `# Placeholder for ServiceMonitor CRD
# Will be populated when Prometheus Operator is used
`
}

func grafanaDashboardConfigMap() string {
	return `apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-dashboards
  namespace: monitoring
data:
  gpu-dashboard.json: |
    {
      "dashboard": {
        "title": "GPU Metrics",
        "panels": [
          {
            "title": "GPU Utilization",
            "type": "graph",
            "targets": [
              {
                "expr": "DCGM_FI_DEV_GPU_UTIL"
              }
            ]
          },
          {
            "title": "GPU Memory Used",
            "type": "graph",
            "targets": [
              {
                "expr": "DCGM_FI_DEV_FB_USED"
              }
            ]
          },
          {
            "title": "GPU Temperature",
            "type": "graph",
            "targets": [
              {
                "expr": "DCGM_FI_DEV_GPU_TEMP"
              }
            ]
          }
        ]
      }
    }
`
}

func grafanaDatasourceConfigMap() string {
	return `apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-datasources
  namespace: monitoring
data:
  datasources.yaml: |
    apiVersion: 1
    datasources:
    - name: Prometheus
      type: prometheus
      access: proxy
      url: http://prometheus:9090
      isDefault: true
      editable: true
`
}

func grafanaDeployment() string {
	return `apiVersion: apps/v1
kind: Deployment
metadata:
  name: grafana
  namespace: monitoring
spec:
  replicas: 1
  selector:
    matchLabels:
      app: grafana
  template:
    metadata:
      labels:
        app: grafana
    spec:
      containers:
      - name: grafana
        image: grafana/grafana:10.2.2
        ports:
        - containerPort: 3000
          name: web
        env:
        - name: GF_SECURITY_ADMIN_PASSWORD
          value: "admin"
        - name: GF_PATHS_PROVISIONING
          value: "/etc/grafana/provisioning"
        volumeMounts:
        - name: datasources
          mountPath: /etc/grafana/provisioning/datasources
        - name: dashboards-config
          mountPath: /etc/grafana/provisioning/dashboards
        - name: dashboards
          mountPath: /var/lib/grafana/dashboards
      volumes:
      - name: datasources
        configMap:
          name: grafana-datasources
      - name: dashboards-config
        configMap:
          name: grafana-dashboards-config
      - name: dashboards
        configMap:
          name: grafana-dashboards
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-dashboards-config
  namespace: monitoring
data:
  dashboards.yaml: |
    apiVersion: 1
    providers:
    - name: 'default'
      orgId: 1
      folder: ''
      type: file
      disableDeletion: false
      updateIntervalSeconds: 10
      options:
        path: /var/lib/grafana/dashboards
`
}

func grafanaService() string {
	return `apiVersion: v1
kind: Service
metadata:
  name: grafana
  namespace: monitoring
spec:
  type: ClusterIP
  ports:
  - port: 3000
    targetPort: 3000
    name: web
  selector:
    app: grafana
`
}

func createNamespace(ctx context.Context, namespace string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "create", "namespace", namespace)
	if err := cmd.Run(); err != nil {
		// Check if it already exists
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

func waitForDeployment(ctx context.Context, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(ctx, "kubectl", "get", "deployment", name,
			"-n", namespace,
			"-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
		output, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(output)) == "True" {
			return nil
		}

		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timeout waiting for deployment %s/%s", namespace, name)
}
