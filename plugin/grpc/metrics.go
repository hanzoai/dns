package grpc

import (
	"github.com/coredns/coredns/plugin"

	metric "github.com/luxfi/metric"
)

// Variables declared for monitoring.
var (
	RequestCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "grpc",
		Name:      "requests_total",
		Help:      "Counter of requests made per upstream.",
	}, []string{"to"})
	RcodeCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "grpc",
		Name:      "responses_total",
		Help:      "Counter of requests made per upstream.",
	}, []string{"rcode", "to"})
	RequestDuration = metric.NewHistogramVec(metric.HistogramOpts{
		Namespace:                   plugin.Namespace,
		Subsystem:                   "grpc",
		Name:                        "request_duration_seconds",
		Buckets:                     plugin.TimeBuckets,
		NativeHistogramBucketFactor: plugin.NativeHistogramBucketFactor,
		Help:                        "Histogram of the time each request took.",
	}, []string{"to"})
)
