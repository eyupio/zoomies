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
		// A twentieth of 16 GB is 819 MB, which is above the 512 MB floor, so
		// the fraction answers for memory here and the floor answers for CPU.
		name: "the floors apply when the operator has set no reserve",
		host: Host{CPUs: 8, MemoryMB: 16384, DiskTotalMB: 100_000, DiskFreeMB: 50_000},
		want: HostAllocation{
			CPUs: 8 - MinHostReserveCPUs, MemoryMB: 16384 - 819, DiskMB: 50_000 - MinHostReserveDiskMB,
			CPUsKnown: true, MemoryKnown: true, DiskKnown: true,
		},
	}, {
		// Both floors grow with the machine: a daemon minding sixty-four
		// containers needs more than the half core and half gigabyte that
		// serve a daemon minding four, and a fixed figure would be a rounding
		// error on a host that size. Memory is capped where holding more back
		// stops buying anything -- a twentieth of 256 GB is 12.8 GB, and the
		// daemon will never want that much.
		name: "both floors are a twentieth of a large machine, and memory is capped",
		host: Host{CPUs: 64, MemoryMB: 262_144},
		want: HostAllocation{
			CPUs: 64 - 64*MinHostReserveCPUFraction, MemoryMB: 262_144 - MaxHostReserveMemoryMB,
			CPUsKnown: true, MemoryKnown: true,
		},
	}, {
		// The flat floor still answers on a small machine, where a twentieth
		// is less than the daemon needs however small the box is.
		name: "the memory floor answers on a small machine",
		host: Host{CPUs: 2, MemoryMB: 4096},
		want: HostAllocation{
			CPUs: 2 - MinHostReserveCPUs, MemoryMB: 4096 - MinHostReserveMemoryMB,
			CPUsKnown: true, MemoryKnown: true,
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
		want: HostAllocation{CPUsKnown: true, MemoryKnown: true, DiskKnown: true, CPUs: 1 - MinHostReserveCPUs},
	}, {
		// A disk that is genuinely full still reported a total, which is what
		// says a measurement happened at all -- the encoding ZF-103a chose.
		name: "a full disk is measured, an unmeasured one is not",
		host: Host{CPUs: 4, MemoryMB: 8192, DiskTotalMB: 100_000, DiskFreeMB: 0},
		want: HostAllocation{
			CPUs: 4 - MinHostReserveCPUs, MemoryMB: 8192 - MinHostReserveMemoryMB,
			CPUsKnown: true, MemoryKnown: true, DiskKnown: true,
		},
	}, {
		name: "a host that measured its size but not its disk",
		host: Host{CPUs: 4, MemoryMB: 8192},
		want: HostAllocation{
			CPUs: 4 - MinHostReserveCPUs, MemoryMB: 8192 - MinHostReserveMemoryMB,
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

// The controller runs on the host whose agent is embedded, so that host holds
// back room for it on top of the floors meant for the daemon and the agent. The
// case that made this necessary: twelve cores held back six tenths of one, and
// with the runners flat out that was all the machine left for the scheduler,
// the database writer and the API as well as dockerd.
//
// The allowance is added to the floor rather than folded into it, and an
// operator's own reserve still wins where it is larger -- someone who held
// back four cores on the controller's host meant four.
func TestTheControllersOwnHostHoldsBackRoomForTheController(t *testing.T) {
	plain := Host{CPUs: 12, MemoryMB: 31 * 1024}
	embedded := plain
	embedded.Embedded = true

	if got, want := embedded.CPUReserve()-plain.CPUReserve(), ControllerReserveCPUs; got != want {
		t.Errorf("the controller's host holds back %.2f more CPU than another, want %.2f", got, want)
	}
	if got, want := embedded.MemoryReserve()-plain.MemoryReserve(), ControllerReserveMemoryMB; got != want {
		t.Errorf("the controller's host holds back %d MB more memory than another, want %d", got, want)
	}
	if got, want := plain.Allocatable().CPUs-embedded.Allocatable().CPUs, ControllerReserveCPUs; got != want {
		t.Errorf("the controller's host has %.2f fewer CPUs to place on, want %.2f: the reserve must reach allocatable", got, want)
	}

	// On a small machine the allowance is still the whole allowance: folding it
	// into a floor the machine already hit would leave the daemon less.
	small := Host{CPUs: 2, MemoryMB: 4096, Embedded: true}
	if got, want := small.CPUReserve(), MinHostReserveCPUs+ControllerReserveCPUs; got != want {
		t.Errorf("a two-core controller host holds back %.2f CPU, want the floor and the allowance, %.2f", got, want)
	}

	operator := embedded
	operator.ReserveCPUs = 4
	if got := operator.CPUReserve(); got != 4 {
		t.Errorf("an operator's reserve of 4 cores on the controller's host became %.2f; the larger figure stands", got)
	}
}
