package cache

import (
	"github.com/coredns/coredns/plugin"

	metric "github.com/luxfi/metric"
)

var (
	// cacheSize is total elements in the cache by cache type.
	cacheSize = metric.NewGaugeVec(metric.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "entries",
		Help:      "The number of elements in the cache.",
	}, []string{"server", "type", "zones", "view"})
	// cacheRequests is a counter of all requests through the cache.
	cacheRequests = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "requests_total",
		Help:      "The count of cache requests.",
	}, []string{"server", "zones", "view"})
	// cacheHits is counter of cache hits by cache type.
	cacheHits = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "hits_total",
		Help:      "The count of cache hits.",
	}, []string{"server", "type", "zones", "view"})
	// cacheMisses is the counter of cache misses. - Deprecated
	cacheMisses = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "misses_total",
		Help:      "The count of cache misses. Deprecated, derive misses from cache hits/requests counters.",
	}, []string{"server", "zones", "view"})
	// cachePrefetches is the number of time the cache has prefetched a cached item.
	cachePrefetches = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "prefetch_total",
		Help:      "The number of times the cache has prefetched a cached item.",
	}, []string{"server", "zones", "view"})
	// cacheDrops is the number responses that are not cached, because the reply is malformed.
	cacheDrops = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "drops_total",
		Help:      "The number responses that are not cached, because the reply is malformed.",
	}, []string{"server", "zones", "view"})
	// servedStale is the number of requests served from stale cache entries.
	servedStale = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "served_stale_total",
		Help:      "The number of requests served from stale cache entries.",
	}, []string{"server", "zones", "view"})
	// evictions is the counter of cache evictions.
	evictions = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "evictions_total",
		Help:      "The count of cache evictions.",
	}, []string{"server", "type", "zones", "view"})
)
