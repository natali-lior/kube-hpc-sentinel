package prometheus

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/api"
	prom_v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

type MetricName string
type NodeName string
type GpuName string
type GpuClusterHealthMetricsScan map[NodeName]map[GpuName]map[MetricName]float32

type HealthRange struct {
	Min float32
	Max float32
}

const (
	GpuTemp         MetricName = "dcgm_gpu_temp"
	MemCopyUtil     MetricName = "dcgm_mem_copy_util"
	FBUsage         MetricName = "dcgm_fb_usage_gpu"
	ECC_SBE_Errs    MetricName = "dcgm_ecc_sbe_aggregate_total"
	PowerUsage      MetricName = "dcgm_power_usage"
	GpuUtil         MetricName = "dcgm_gpu_util"
	NvlinkBandwidth MetricName = "dcgm_nvlink_bandwidth_total"
)

var (
	GpuClusterHealthScanPrmQuery = "{service=\"dcgm-mock-exporter\", node=~\".+\"}"
	HealthMetrics                = map[MetricName]HealthRange{
		GpuTemp:         {Min: 35, Max: 85},
		MemCopyUtil:     {Min: 0.1, Max: 0.6},
		FBUsage:         {Min: 0.2, Max: 0.7},
		ECC_SBE_Errs:    {Min: 0, Max: 0},
		PowerUsage:      {Min: 100, Max: 250},
		GpuUtil:         {Min: 0.3, Max: 0.8},
		NvlinkBandwidth: {Min: 250, Max: 400},
	}
)

type MetricsProvider struct {
	prom_api prom_v1.API
}

func NewMetricsProvider(address string) (*MetricsProvider, error) {
	client, err := api.NewClient(api.Config{Address: address})
	if err != nil {
		return nil, err
	}
	return &MetricsProvider{prom_api: prom_v1.NewAPI(client)}, nil
}

func (m *MetricsProvider) GetFullGPUClusterHealthCheck(ctx context.Context) (GpuClusterHealthMetricsScan, error) {
	r, warn, err := m.prom_api.Query(ctx, GpuClusterHealthScanPrmQuery, time.Now())
	if err != nil {
		return nil, err
	}
	if len(warn) > 0 {
		log.Ctx(ctx).Warn().Msgf("prom client execution warnings: %v\n", warn)
	}
	vec, ok := r.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("expected prometheus vector, got %v | %T", r, r.Type())
	}
	scan := make(GpuClusterHealthMetricsScan)
	for _, sample := range vec {
		node := NodeName(sample.Metric["node"])
		gpu := GpuName(sample.Metric["gpu"])
		metricName := MetricName(sample.Metric["__name__"])
		val := float32(sample.Value)
		if scan[node] == nil {
			scan[node] = make(map[GpuName]map[MetricName]float32)
		}
		if scan[node][gpu] == nil {
			scan[node][gpu] = make(map[MetricName]float32)
		}
		scan[node][gpu][metricName] = val
	}

	return scan, nil
}

func (m *MetricsProvider) GetFullGPUNodeHealthCheck(ctx context.Context, nodeName, gpuName string) (map[MetricName]float64, error) {
	l := log.Ctx(ctx).With().Str("node", nodeName).Str("gpu", gpuName).Logger()
	g, ctx := errgroup.WithContext(ctx)
	health := map[MetricName]float64{}
	for healthMetric := range HealthMetrics {
		g.Go(func() error {
			val, err := m.GetDCGMMetric(ctx, healthMetric, nodeName, gpuName)
			if err == nil {
				health[healthMetric] = val
			}
			return err
		})
	}
	if err := g.Wait(); err != nil {
		l.Error().Err(err).Msgf("failed to gather full gpu health report for node [%s] gpu [%s] %v", nodeName, gpuName, err)
		return nil, err
	}
	return health, nil
}

func (m *MetricsProvider) GetDCGMMetric(ctx context.Context, metric MetricName, nodeName string, gpuName string) (float64, error) {
	query := fmt.Sprintf("%s{node=\"%s\", gpu=\"%s\"}", string(metric), nodeName, gpuName)
	return m.executeNodeGpuMetric(ctx, query, nodeName, gpuName)
}

func (m *MetricsProvider) executeNodeGpuMetric(ctx context.Context, query, nodeName, gpuName string) (float64, error) {
	l := log.Ctx(ctx)
	val, warns, err := m.prom_api.Query(ctx, query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("error querying prometheus: %v", err)
	}
	if len(warns) > 0 {
		l.Warn().Msgf("prometheus warnings: %v\n", warns)
	}
	res, err := modelValToFloatVal(val)
	if err != nil {
		return 0, fmt.Errorf("node [%s], gpu [%s]: %v", nodeName, gpuName, err)
	}
	return res, nil
}

func modelValToFloatVal(val model.Value) (float64, error) {
	switch v := val.(type) {
	case model.Vector:
		if len(v) == 0 {
			return 0, fmt.Errorf("no temperature metrics found")
		}
		return float64(v[0].Value), nil
	case *model.Scalar:
		return float64(v.Value), nil
	default:
		return 0, fmt.Errorf("unexpected prometheus result type: %T", val)
	}
}
