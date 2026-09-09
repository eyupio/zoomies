package store

import "testing"

// Allocatable is the arithmetic every placement decision starts from, and the
// distinction it has to keep is between a figure that was measured and one
// that never was. Zero means both things on a host row, and reading "nobody
// looked" as "nothing left" would refuse work on every host of a fleet the
// moment it upgraded past the agents that report these.
func TestAllocatableSeparatesUnmeasuredFromEmpty(t *testing.T) {
	cases := []struct {
		name string
		host Host
		want HostAllocation
	}{{
		name: "an agent that reports nothing has measured nothing",
		host: Host{},
		want: HostAllocation{},
	}, {
		name: "the floors apply when the operator has set no reserve",
		host: Host{CPUs: 8, MemoryMB: 16384, DiskTotalMB: 100_000, DiskFreeMB: 50_000},
		want: HostAllocation{
			CPUs: 8, MemoryMB: 16384 - MinHostReserveMemoryMB, DiskMB: 50_000 - MinHostReserveDiskMB,
			CPUsKnown: true, MemoryKnown: true, DiskKnown: true,
		},
	}, {
		name: "an operator's larger reserve wins over the floor",
		host: Host{
			CPUs: 8, MemoryMB: 16384, DiskTotalMB: 100_000, DiskFreeMB: 50_000,
			ReserveCPUs: 2, ReserveMemoryMB: 4096, ReserveDiskMB: 20_000,
		},
		want: HostAllocation{
			CPUs: 6, MemoryMB: 12288, DiskMB: 30_000,
			CPUsKnown: true, MemoryKnown: true, DiskKnown: true,
		},
	}, {
		// The failure this prevents is a negative allocation reading as a very
		// large one somewhere downstream. A machine smaller than its own
		// reserve has no room to run a job in, and saying zero is the honest
		// answer rather than a bug.
		name: "a host smaller than its reserve allocates nothing, and still counts as measured",
		host: Host{CPUs: 1, MemoryMB: 256, DiskTotalMB: 8000, DiskFreeMB: 100},
		want: HostAllocation{CPUsKnown: true, MemoryKnown: true, DiskKnown: true, CPUs: 1},
	}, {
		// A disk that is genuinely full still reported a total, which is what
		// says a measurement happened at all -- the encoding ZF-103a chose.
		name: "a full disk is measured, an unmeasured one is not",
		host: Host{CPUs: 4, MemoryMB: 8192, DiskTotalMB: 100_000, DiskFreeMB: 0},
		want: HostAllocation{
			CPUs: 4, MemoryMB: 8192 - MinHostReserveMemoryMB,
			CPUsKnown: true, MemoryKnown: true, DiskKnown: true,
		},
	}, {
		name: "a host that measured its size but not its disk",
		host: Host{CPUs: 4, MemoryMB: 8192},
		want: HostAllocation{
			CPUs: 4, MemoryMB: 8192 - MinHostReserveMemoryMB,
			CPUsKnown: true, MemoryKnown: true,
		},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.host.Allocatable(); got != tc.want {
				t.Errorf("Allocatable() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
