package object

import (
	"testing"
	"time"

	metric "github.com/luxfi/metric"
	api "k8s.io/api/core/v1"
)

// histSampleCount gathers reg and returns the observation count of the named
// histogram for the given service_kind label. A label with no observations is
// absent from the gathered families, so it reports 0.
func histSampleCount(t *testing.T, reg metric.Registry, name, label string) uint64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range families {
		if mf.Name != name {
			continue
		}
		for _, m := range mf.Metrics {
			for _, lp := range m.Labels {
				if lp.Name == "service_kind" && lp.Value == label {
					return m.Value.SampleCount
				}
			}
		}
	}
	return 0
}

// NOTE: subtests in this function must NOT call t.Parallel() — they swap
// global package-level vars (DNSProgrammingLatency, DurationSinceFunc).
func TestEndpointLatencyRecorder_record(t *testing.T) {
	tests := []struct {
		name            string
		services        []*Service
		ttSet           bool
		wantLabel       string
		wantSampleCount uint64
	}{
		{
			name:            "headless_with_selector: headless service with trigger annotation",
			services:        []*Service{{ClusterIPs: []string{api.ClusterIPNone}}},
			ttSet:           true,
			wantLabel:       "headless_with_selector",
			wantSampleCount: 1,
		},
		{
			name:            "cluster_ip: ClusterIP service with trigger annotation",
			services:        []*Service{{ClusterIPs: []string{"10.0.0.1"}}},
			ttSet:           true,
			wantLabel:       "cluster_ip",
			wantSampleCount: 1,
		},
		{
			name:            "no annotation on headless: TT zero means no observation",
			services:        []*Service{{ClusterIPs: []string{api.ClusterIPNone}}},
			ttSet:           false,
			wantLabel:       "headless_with_selector",
			wantSampleCount: 0,
		},
		{
			name:            "no annotation on ClusterIP: TT zero means no observation",
			services:        []*Service{{ClusterIPs: []string{"10.0.0.1"}}},
			ttSet:           false,
			wantLabel:       "cluster_ip",
			wantSampleCount: 0,
		},
		{
			name:            "informer lag: no backing service found, TT set, no observation",
			services:        nil,
			ttSet:           true,
			wantLabel:       "cluster_ip",
			wantSampleCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Replace global metric with a fresh unregistered histogram for isolation.
			// Do NOT add t.Parallel() here — these subtests swap global package state.
			origMetric := DNSProgrammingLatency
			reg := metric.NewRegistry()
			DNSProgrammingLatency = metric.With(reg).NewHistogramVec(metric.HistogramOpts{
				Name:    "test_dns_programming_duration_seconds",
				Help:    "test histogram",
				Buckets: metric.ExponentialBuckets(0.001, 2, 20),
			}, []string{"service_kind"})
			t.Cleanup(func() { DNSProgrammingLatency = origMetric })

			origDurationSince := DurationSinceFunc
			DurationSinceFunc = func(time.Time) time.Duration { return time.Second }
			t.Cleanup(func() { DurationSinceFunc = origDurationSince })

			rec := &EndpointLatencyRecorder{Services: tc.services}
			if tc.ttSet {
				rec.TT = time.Now().Add(-time.Second)
			}

			rec.record()

			got := histSampleCount(t, reg, "test_dns_programming_duration_seconds", tc.wantLabel)
			if got != tc.wantSampleCount {
				t.Errorf("sample count for label %q = %d, want %d", tc.wantLabel, got, tc.wantSampleCount)
			}
		})
	}
}
