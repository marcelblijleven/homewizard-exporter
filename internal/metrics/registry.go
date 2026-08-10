// Package metrics owns the Prometheus registry and the descriptors
// homewizard_exporter publishes.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/marcelblijleven/homewizard_exporter/internal/buildinfo"
)

// Namespace prefixes every metric this exporter publishes.
const Namespace = "homewizard"

// NewRegistry returns a registry pre-loaded with the Go runtime
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		NewBuildInfoCollector(),
	)
	return reg
}

// NewBuildInfoCollector exports a constant 1 labelled with the build details,
// so dashboards and alerts can pin down which binary produced a series
func NewBuildInfoCollector() prometheus.Collector {
	bi := buildinfo.Get()
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "build_info",
		Help:      "Build information for the running homewizard_exporter binary. Always 1.",
		ConstLabels: prometheus.Labels{
			"version":   bi.Version,
			"commit":    bi.Commit,
			"goversion": bi.GoVersion,
		},
	}, func() float64 { return 1 })
}
