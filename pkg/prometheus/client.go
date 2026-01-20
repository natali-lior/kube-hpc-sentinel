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

type MetricType string

const (
	GpuTemp         MetricType = "dcgm_gpu_temp"
	MemCopyUtil     MetricType = "dcgm_mem_copy_util"
	FBUsage         MetricType = "dcgm_fb_usage_gpu"
	ECC_SBE_Errs    MetricType = "dcgm_ecc_sbe_aggregate_total"
	PowerUsage      MetricType = "dcgm_power_usage"
	GpuUtil         MetricType = "dcgm_gpu_util"
	NvlinkBandwidth MetricType = "dcgm_nvlink_bandwidth_total"
)

var (
	HealthMetrics []MetricType = []MetricType{
		GpuTemp,
		MemCopyUtil,
		FBUsage,
		ECC_SBE_Errs,
		PowerUsage,
		GpuUtil,
		NvlinkBandwidth,
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

func (m *MetricsProvider) GetFullGPUNodeHealthCheck(ctx context.Context, nodeName, gpuName string) (map[MetricType]float64, error) {
	l := log.Ctx(ctx).With().Str("node", nodeName).Str("gpu", gpuName).Logger()
	g, ctx := errgroup.WithContext(ctx)
	health := map[MetricType]float64{}
	for _, healthMetric := range HealthMetrics {
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

func (m *MetricsProvider) GetDCGMMetric(ctx context.Context, metric MetricType, nodeName string, gpuName string) (float64, error) {
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
