package hosts

import (
	"github.com/coredns/coredns/plugin"

	metric "github.com/luxfi/metric"
)

var (
	// hostsEntries is the combined number of entries in hosts and Corefile.
	hostsEntries = metric.NewGaugeVec(metric.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: "hosts",
		Name:      "entries",
		Help:      "The combined number of entries in hosts and Corefile.",
	}, []string{"hostsfile"})
	// hostsReloadTime is the timestamp of the last reload of hosts file.
	hostsReloadTime = metric.NewGauge(metric.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: "hosts",
		Name:      "reload_timestamp_seconds",
		Help:      "The timestamp of the last reload of hosts file.",
	})
)
