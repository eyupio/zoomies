package agent

import (
	"testing"
	"time"
)

func TestRecoveryJitterSpreadsHostsWithoutEarlyRetry(t *testing.T) {
	buckets := map[time.Duration]int{}
	for i := 0; i < 1000; i++ {
		d := recoveryDelay(time.Minute, float64(i)/999)
		if d < time.Minute || d > 75*time.Second {
			t.Fatalf("delay %v", d)
		}
		buckets[d/time.Second]++
	}
	for _, n := range buckets {
		if n > 70 {
			t.Fatalf("recovery peak %d of 1000 hosts", n)
		}
	}
	if len(buckets) < 15 {
		t.Fatal("jitter did not distribute recovery")
	}
}
