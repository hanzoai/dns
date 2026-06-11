package reload

import (
	"github.com/coredns/coredns/plugin"

	metric "github.com/luxfi/metric"
)

// Metrics for the reload plugin
var (
	// failedCount is the counter of the number of failed reload attempts.
	failedCount = metric.NewCounter(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "reload",
		Name:      "failed_total",
		Help:      "Counter of the number of failed reload attempts.",
	})
	// reloadInfo is record the hash value during reload.
	reloadInfo = metric.NewGaugeVec(metric.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: "reload",
		Name:      "version_info",
		Help:      "A metric with a constant '1' value labeled by hash, and value which type of hash generated.",
	}, []string{"hash", "value"})
)
