package local

import (
	"github.com/coredns/coredns/plugin"

	metric "github.com/luxfi/metric"
)

var (
	// LocalhostCount report the number of times we've seen a localhost.<domain> query.
	LocalhostCount = metric.NewCounter(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "local",
		Name:      "localhost_requests_total",
		Help:      "Counter of localhost.<domain> requests.",
	})
)
