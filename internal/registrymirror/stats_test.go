package registrymirror

import (
	"encoding/json"
	"testing"
)

// The figures cross the wire in an agent's heartbeat and are read by a
// controller that is upgraded separately, so their names are a contract and not
// whatever the field names happen to be. A renamed field would arrive as a zero
// and read as a mirror that had never served anything.
func TestTheFiguresAreNamedForTheWire(t *testing.T) {
	raw, err := json.Marshal(Stats{
		Requests: 1, Hits: 2, Misses: 3, UpstreamErrors: 4, UpstreamFailingSeconds: 9, Evictions: 5,
		BytesServed: 6, BytesFetched: 7, BytesStored: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"requests":1,"hits":2,"misses":3,"upstream_errors":4,"upstream_failing_seconds":9,"evictions":5,"bytes_served":6,"bytes_fetched":7,"bytes_stored":8}`
	if string(raw) != want {
		t.Fatalf("figures on the wire:\n got %s\nwant %s", raw, want)
	}
}
