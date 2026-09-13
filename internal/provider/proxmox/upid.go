package proxmox

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// UPID is Proxmox's identifier for one asynchronous task.
//
// It is the durable operation handle this provider returns from every call that
// starts work: a clone, a start, a shutdown, a delete. Everything needed to ask
// about the task later is inside the string, the node included, so a controller
// that restarts holding nothing but the stored handle can still find out what
// happened rather than doing the work again.
//
// The wire form is eight colon-separated fields and a trailing colon:
//
//	UPID:pve-1:00051234:0089ABCD:65F0A1B2:qmclone:143:zoomies@pve!fleet:
//	     node  pid      pstart   start    type    id  user
//
// pstart is eight or nine hex digits -- nine on a node that has been up long
// enough for the kernel's boot-relative counter to overflow eight -- which is
// the detail a parser written from one example gets wrong, and gets wrong only
// on the machines that have been running longest.
type UPID struct {
	// Raw is the string exactly as Proxmox wrote it. It is what is stored and
	// what is sent back, because a handle rewritten from its parts is a handle
	// that can differ from the one the task actually has.
	Raw       string
	Node      string
	PID       uint64
	PStart    uint64
	StartTime time.Time
	// Type is the worker kind: "qmclone", "qmstart", "qmshutdown", "qmdestroy".
	Type string
	// ID is what the task is about -- a VMID for a guest task -- and is empty
	// for tasks that are about nothing in particular.
	ID   string
	User string
}

// String returns the handle as Proxmox wrote it.
func (u UPID) String() string { return u.Raw }

// Zero reports whether this is the absent handle.
func (u UPID) Zero() bool { return u.Raw == "" }

// ParseUPID reads a task handle.
//
// It validates the shape -- the prefix, the field count, the trailing colon,
// and that the three numeric fields are hexadecimal -- and deliberately does
// not validate the rest. The worker types are Proxmox's to add to, and a
// release that invents one must not make a task we are already waiting on
// unreadable.
//
// A caller that only wants the node should not depend on this succeeding: the
// node a machine's resource lives on is recorded on its own row, and that is
// the answer to fall back on. Refusing to poll a task because its handle looked
// unfamiliar would strand a real virtual machine, which is the one outcome
// nothing here may cause.
func ParseUPID(s string) (UPID, error) {
	const prefix = "UPID:"
	if !strings.HasPrefix(s, prefix) {
		return UPID{}, fmt.Errorf("proxmox: %q is not a task handle; one starts with %q", elide(s), prefix)
	}
	if !strings.HasSuffix(s, ":") {
		return UPID{}, fmt.Errorf("proxmox: task handle %q is cut short; one ends with a colon", elide(s))
	}
	// The trailing colon closes the last field rather than opening another, so
	// it is dropped before splitting; eight fields remain, the first of which
	// is the "UPID" marker itself.
	parts := strings.Split(strings.TrimSuffix(s, ":"), ":")
	if len(parts) != 8 {
		return UPID{}, fmt.Errorf("proxmox: task handle %q has %d fields, want 8", elide(s), len(parts))
	}

	u := UPID{Raw: s, Node: parts[1], Type: parts[5], ID: parts[6], User: parts[7]}
	if u.Node == "" {
		return UPID{}, fmt.Errorf("proxmox: task handle %q names no node, so there is nowhere to ask about it", elide(s))
	}
	var err error
	if u.PID, err = hexField(s, "process id", parts[2]); err != nil {
		return UPID{}, err
	}
	if u.PStart, err = hexField(s, "process start", parts[3]); err != nil {
		return UPID{}, err
	}
	started, err := hexField(s, "start time", parts[4])
	if err != nil {
		return UPID{}, err
	}
	u.StartTime = time.Unix(int64(started), 0).UTC()
	return u, nil
}

func hexField(upid, name, text string) (uint64, error) {
	n, err := strconv.ParseUint(text, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("proxmox: task handle %q has %s %q, which is not hexadecimal", elide(upid), name, text)
	}
	return n, nil
}

// elide keeps a malformed handle out of a log line at full length. Whatever
// produced it is not necessarily Proxmox, and an error message is not the place
// to reprint an arbitrary string somebody else chose.
func elide(s string) string {
	const max = 80
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
