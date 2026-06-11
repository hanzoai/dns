package autopath

import (
	"github.com/coredns/coredns/plugin"

	metric "github.com/luxfi/metric"
)

var (
	// autoPathCount is counter of successfully autopath-ed queries.
	autoPathCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "autopath",
		Name:      "success_total",
		Help:      "Counter of requests that did autopath.",
	}, []string{"server"})
)
