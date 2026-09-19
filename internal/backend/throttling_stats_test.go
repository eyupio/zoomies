package backend

import (
	"encoding/json"
	"testing"
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
