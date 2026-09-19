package backend

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCPUThrottlingCountersDistinguishUnsupportedFromZero(t *testing.T) {
	for _, tc := range []struct {
		payload   string
		supported bool
		ns        uint64
	}{
		{`{"cpu_stats":{}}`, false, 0},
		{`{"cpu_stats":{"throttling_data":{"periods":0,"throttled_periods":0,"throttled_time":0}}}`, true, 0},
		{`{"cpu_stats":{"throttling_data":{"periods":100,"throttled_periods":70,"throttled_time":9000000000}}}`, true, 9000000000},
	} {
		var raw statsJSON
		if err := json.Unmarshal([]byte(tc.payload), &raw); err != nil {
			t.Fatal(err)
		}
		got := raw.sample()
		if (got.CPUThrottling != nil) != tc.supported {
			t.Fatalf("capability: %+v", got)
		}
		if tc.supported && got.CPUThrottling.ThrottledNanoseconds != tc.ns {
			t.Fatalf("counter: %+v", got)
		}
	}
}

func TestDinDStatsAddIntoOneLogicalRunner(t *testing.T) {
	runnerAt := time.Unix(10, 0)
	daemonAt := runnerAt.Add(time.Second)
	got := addStats(
		Stats{SampledAt: &runnerAt, CPUPercent: 40, MemoryBytes: 100, MemoryLimit: 200, CPUThrottling: &CPUThrottling{Periods: 10, ThrottledPeriods: 2, ThrottledNanoseconds: 3}},
		Stats{SampledAt: &daemonAt, CPUPercent: 160, MemoryBytes: 300, MemoryLimit: 400, CPUThrottling: &CPUThrottling{Periods: 20, ThrottledPeriods: 4, ThrottledNanoseconds: 5}},
	)
	if got.CPUPercent != 200 || got.MemoryBytes != 400 || got.MemoryLimit != 600 || got.SampledAt == nil || !got.SampledAt.Equal(daemonAt) {
		t.Fatalf("combined stats = %+v", got)
	}
	if got.CPUThrottling == nil || got.CPUThrottling.Periods != 30 || got.CPUThrottling.ThrottledPeriods != 6 || got.CPUThrottling.ThrottledNanoseconds != 8 {
		t.Fatalf("combined throttling = %+v", got.CPUThrottling)
	}
}
