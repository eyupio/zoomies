package proxmox

import (
	"strings"
	"testing"
	"time"
)

// The handle is how a controller that restarted finds out what happened to work
// it started, so the cases that matter are the ones that would make a real task
// unreadable rather than the ones that look untidy.
func TestATaskHandleIsReadWithoutLosingTheTaskItNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want UPID
	}{
		{
			name: "a clone",
			raw:  "UPID:pve-1:00051234:0089ABCD:65F0A1B2:qmclone:143:zoomies@pve!fleet:",
			want: UPID{
				Node: "pve-1", PID: 0x51234, PStart: 0x89ABCD,
				StartTime: time.Unix(0x65F0A1B2, 0).UTC(),
				Type:      "qmclone", ID: "143", User: "zoomies@pve!fleet",
			},
		},
		{
			// Proxmox writes eight or nine hex digits here: nine once the node
			// has been up long enough for the counter to need them. A parser
			// written against one example fails on exactly the machines that
			// have been running longest, which is the worst possible set.
			name: "a node that has been up a long time",
			raw:  "UPID:pve-1:00051234:1089ABCDE:65F0A1B2:qmstart:143:root@pam:",
			want: UPID{
				Node: "pve-1", PID: 0x51234, PStart: 0x1089ABCDE,
				StartTime: time.Unix(0x65F0A1B2, 0).UTC(),
				Type:      "qmstart", ID: "143", User: "root@pam",
			},
		},
		{
			// A task about nothing in particular leaves the field empty rather
			// than omitting it, so the field count is still eight.
			name: "a task about nothing in particular",
			raw:  "UPID:pve-1:00051234:0089ABCD:65F0A1B2:srvreload::root@pam:",
			want: UPID{
				Node: "pve-1", PID: 0x51234, PStart: 0x89ABCD,
				StartTime: time.Unix(0x65F0A1B2, 0).UTC(),
				Type:      "srvreload", ID: "", User: "root@pam",
			},
		},
		{
			name: "a hyphenated node name",
			raw:  "UPID:pve-node-02:0000ffff:00000001:65F0A1B2:qmdestroy:9001:zoomies@pve:",
			want: UPID{
				Node: "pve-node-02", PID: 0xffff, PStart: 1,
				StartTime: time.Unix(0x65F0A1B2, 0).UTC(),
				Type:      "qmdestroy", ID: "9001", User: "zoomies@pve",
			},
		},
		{
			// Nothing here knows the full list of worker types, and a Proxmox
			// release that adds one must not make a task we are waiting on
			// unreadable.
			name: "a worker type this build has never heard of",
			raw:  "UPID:pve-1:00051234:0089ABCD:65F0A1B2:somethingnew:143:root@pam:",
			want: UPID{
				Node: "pve-1", PID: 0x51234, PStart: 0x89ABCD,
				StartTime: time.Unix(0x65F0A1B2, 0).UTC(),
				Type:      "somethingnew", ID: "143", User: "root@pam",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseUPID(tc.raw)
			if err != nil {
				t.Fatalf("ParseUPID(%q): %v", tc.raw, err)
			}
			tc.want.Raw = tc.raw
			if got != tc.want {
				t.Errorf("ParseUPID(%q)\n got %+v\nwant %+v", tc.raw, got, tc.want)
			}
			// The handle is sent back to Proxmox exactly as it arrived: one
			// rebuilt from its parts can differ from the one the task has.
			if got.String() != tc.raw {
				t.Errorf("String() = %q, want the handle unchanged", got.String())
			}
		})
	}
}

// A malformed handle is refused with a message that says which part of it is
// wrong, because the thing that produced it is not necessarily Proxmox.
func TestAHandleThatIsNotATaskIsRefusedAndSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		raw   string
		wants string
	}{
		{"empty", "", "starts with"},
		{"another identifier entirely", "mach_k3f9qz2mx7ab", "starts with"},
		{"cut short", "UPID:pve-1:00051234:0089ABCD:65F0A1B2:qmclone:143:root@pam", "cut short"},
		{"too few fields", "UPID:pve-1:00051234:qmclone:", "fields"},
		{"too many fields", "UPID:pve-1:00051234:0089ABCD:65F0A1B2:qmclone:143:root@pam:extra:", "fields"},
		{"no node", "UPID::00051234:0089ABCD:65F0A1B2:qmclone:143:root@pam:", "names no node"},
		{"a process id that is not a number", "UPID:pve-1:notahexnum:0089ABCD:65F0A1B2:qmclone:143:root@pam:", "hexadecimal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseUPID(tc.raw)
			if err == nil {
				t.Fatalf("ParseUPID(%q) was accepted", tc.raw)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error was %q, want it to mention %q", err, tc.wants)
			}
		})
	}
}

// An arbitrary string is not reprinted at full length into a log line or an
// error an operator reads.
func TestARefusalDoesNotReprintAWholeUnreasonableString(t *testing.T) {
	_, err := ParseUPID(strings.Repeat("x", 4096))
	if err == nil {
		t.Fatal("4 KiB of nonsense was accepted as a task handle")
	}
	if len(err.Error()) > 200 {
		t.Errorf("the refusal was %d bytes long; it quotes its input without limit", len(err.Error()))
	}
}
