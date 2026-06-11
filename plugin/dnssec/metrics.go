package dnssec

import (
	"github.com/coredns/coredns/plugin"

	metric "github.com/luxfi/metric"
)

var (
	// cacheSize is the number of elements in the dnssec cache.
	cacheSize = metric.NewGaugeVec(metric.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: "dnssec",
		Name:      "cache_entries",
		Help:      "The number of elements in the dnssec cache.",
	}, []string{"server", "type"})
	// cacheHits is the count of cache hits.
	cacheHits = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "dnssec",
		Name:      "cache_hits_total",
		Help:      "The count of cache hits.",
	}, []string{"server"})
	// cacheMisses is the count of cache misses.
	cacheMisses = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "dnssec",
		Name:      "cache_misses_total",
		Help:      "The count of cache misses.",
	}, []string{"server"})
)
