package dns64

import (
	"github.com/coredns/coredns/plugin"

	metric "github.com/luxfi/metric"
)

var (
	// RequestsTranslatedCount is the number of DNS requests translated by dns64.
	RequestsTranslatedCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "requests_translated_total",
		Help:      "Counter of DNS requests translated by dns64.",
	}, []string{"server"})
)
