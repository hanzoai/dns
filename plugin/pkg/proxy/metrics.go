package proxy

import (
	"github.com/coredns/coredns/plugin"

	metric "github.com/luxfi/metric"
)

// Variables declared for monitoring.
var (
	requestDuration = metric.NewHistogramVec(metric.HistogramOpts{
		Namespace:                   plugin.Namespace,
		Subsystem:                   "proxy",
		Name:                        "request_duration_seconds",
		Buckets:                     plugin.TimeBuckets,
		NativeHistogramBucketFactor: plugin.NativeHistogramBucketFactor,
		Help:                        "Histogram of the time each request took.",
	}, []string{"proxy_name", "to", "rcode"})

	healthcheckFailureCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "proxy",
		Name:      "healthcheck_failures_total",
		Help:      "Counter of the number of failed healthchecks.",
	}, []string{"proxy_name", "to"})

	connCacheHitsCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "proxy",
		Name:      "conn_cache_hits_total",
		Help:      "Counter of connection cache hits per upstream and protocol.",
	}, []string{"proxy_name", "to", "proto"})

	connCacheMissesCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "proxy",
		Name:      "conn_cache_misses_total",
		Help:      "Counter of connection cache misses per upstream and protocol.",
	}, []string{"proxy_name", "to", "proto"})
)
