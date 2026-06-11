package nomad

import (
	"github.com/coredns/coredns/plugin"

	metric "github.com/luxfi/metric"
)

var (
	// requestSuccessCount is the number of DNS requests handled successfully.
	requestSuccessCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "success_requests_total",
		Help:      "Counter of DNS requests handled successfully.",
	}, []string{"server", "namespace"})
	// requestFailedCount is the number of DNS requests that failed.
	requestFailedCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "failed_requests_total",
		Help:      "Counter of DNS requests failed.",
	}, []string{"server", "namespace"})
)
