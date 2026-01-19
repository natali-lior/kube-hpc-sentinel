package prometheus

import (
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
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

func (m *MetricsProvider) GetNodeTemperature(nodeName string) (float64, error) {
	// Implement your PromQL query here:
	// "nvidia_gpu_temperature_celsius{node='%s'}"
	return 0, nil
}
