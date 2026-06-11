package template

import (
	"github.com/coredns/coredns/plugin"

	metric "github.com/luxfi/metric"
)

var (
	// templateMatchesCount is the counter of template regex matches.
	templateMatchesCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "template",
		Name:      "matches_total",
		Help:      "Counter of template regex matches.",
	}, []string{"server", "zone", "view", "class", "type"})
	// templateFailureCount is the counter of go template failures.
	templateFailureCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "template",
		Name:      "template_failures_total",
		Help:      "Counter of go template failures.",
	}, []string{"server", "zone", "view", "class", "type", "section", "template"})
	// templateRRFailureCount is the counter of mis-templated RRs.
	templateRRFailureCount = metric.NewCounterVec(metric.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "template",
		Name:      "rr_failures_total",
		Help:      "Counter of mis-templated RRs.",
	}, []string{"server", "zone", "view", "class", "type", "section", "template"})
)
