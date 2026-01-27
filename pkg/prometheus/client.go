package prometheus

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

type MetricName string
type NodeName string
type GpuName string
type GpuClusterHealthMetricsScan map[NodeName]map[GpuName]map[MetricName]float32

type HealthRange struct {
	min float32
	max float32
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
		GpuTemp:         {min: 35, max: 85},
		MemCopyUtil:     {min: 0.1, max: 0.6},
		FBUsage:         {min: 0.2, max: 0.7},
		ECC_SBE_Errs:    {min: 0, max: 0},
		PowerUsage:      {min: 100, max: 250},
		GpuUtil:         {min: 0.3, max: 0.8},
		NvlinkBandwidth: {min: 250, max: 400},
	}
)

type MetricsProvider struct {
	api v1.API
}

func NewMetricsProvider(address string) (*MetricsProvider, error) {
	client, err := api.NewClient(api.Config{Address: address})
	if err != nil {
		return nil, err
	}
	return &MetricsProvider{api: v1.NewAPI(client)}, nil
}

func (m *MetricsProvider) GetFullGPUClusterHealthCheck(ctx context.Context) (GpuClusterHealthMetricsScan, error) {
	GpuClusterHealthMetricsScan := map[NodeName]map[GpuName]map[MetricName]float32{}
	return GpuClusterHealthMetricsScan, nil
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
	val, warns, err := m.api.Query(ctx, query, time.Now())
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
