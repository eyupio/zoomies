package proxmox

// Bootstrap: how the enrolment payload gets inside a machine.
//
// It goes through the QEMU guest agent, and the reason is not preference. The
// usual way to hand a guest arbitrary user-data is
// cicustom=user=<storage>:snippets/<file>, and that cannot be driven from the
// API at all: the storage upload endpoint accepts content of iso, vztmpl or
// import, and snippets is not in the enum. Every tool that uses cicustom places
// the file over SSH, which would mean Zoomies holding shell credentials to a
// hypervisor -- a far larger trust boundary than a scoped API token, and one
// this integration is not willing to ask for. The cloud-init fields that are
// settable through the API carry no arbitrary user-data.
//
// What the guest agent buys, beyond being reachable at all:
//
//   - The credential never enters the machine's metadata, so it is not readable
//     by a Proxmox user holding VM.Config.Cloudinit or VM.Audit, and never
//     lands in a seed image on shared storage.
//   - exec-status returns the guest's own exit code and standard error, which
//     becomes the machine's failure reason. "The agent did not install" is not
//     something anybody can act on; "zoomies-agent.service not found" is.
//
// What it costs is two privileges -- VM.GuestAgent.FileWrite and
// VM.GuestAgent.Unrestricted -- and a template that ships qemu-guest-agent.
// Preflight names both, because a template prepared without them produces
// machines that boot, cost money and never join.
//
// Nothing here builds a shell command line. Commands are argv arrays the whole
// way down, so there is no quoting to get wrong and no injection class to
// police.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/provider"
)

const (
	// guestAgentPollInterval is how often a guest that has just started is
	// asked whether its agent is up. A guest answers within seconds of
	// finishing its boot, and giving up on the first refusal would spend a
	// whole reconcile backoff on a machine that was two seconds from ready.
	guestAgentPollInterval = 2 * time.Second
	// guestExecPollInterval is how often a command running inside the guest is
	// asked whether it has finished. The commands here are a chmod and a
	// systemctl, which is to say milliseconds.
	guestExecPollInterval = 500 * time.Millisecond
	// maxAgentFileBytes is what the API will carry in one file-write. Checking
	// it here turns "400: value is too long" into a sentence naming the file.
	maxAgentFileBytes = 61440
)

// chmodCommand is the program that fixes a file's mode. It is an absolute path
// because there is no shell in this path to search one.
const chmodCommand = "/bin/chmod"

// execHandlePrefix marks an operation handle that is a process inside a guest
// rather than a cluster task. The guest agent has no UPID to give -- it returns
// a process id, which means nothing without the node and the guest it belongs
// to -- so the handle carries all three. It is durable in the only sense that
// matters here: a controller that restarts can still ask what the command did.
const execHandlePrefix = "guest-exec:"

// Bootstrap writes the enrolment payload into a running guest and starts the
// agent.
//
// The handle it returns follows the last command, which is the one that starts
// the agent and the only one whose outcome a machine's life depends on. The
// preparation before it -- the files, and the chmod each one needs -- is
// awaited here, because a handle for each would need somewhere to be stored and
// there is one column for one handle.
func (p *Provider) Bootstrap(ctx context.Context, ref provider.MachineRef, payload provider.Bootstrap) (provider.OperationRef, error) {
	node, vmid, err := p.locate(ref, "install the agent inside a machine")
	if err != nil {
		return provider.OperationRef{}, err
	}
	if _, set := ctx.Deadline(); !set {
		// This is the one call here that waits for something, so it is the one
		// that needs a floor under it: a caller that set no deadline would
		// otherwise have this poll a guest that is never going to answer for
		// as long as the process lives. The budget is this provider's own.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.caps.Deadlines.Bootstrap)
		defer cancel()
	}
	if err := p.awaitGuestAgent(ctx, node, vmid); err != nil {
		return provider.OperationRef{}, err
	}

	for _, f := range payload.Files {
		if len(f.Content) > maxAgentFileBytes {
			return provider.OperationRef{}, &provider.Error{
				Kind: provider.FailureConfig, Op: "write a file inside a machine", Ref: vmRef(node, vmid),
				Message: fmt.Sprintf("%s is %d bytes and the guest agent carries at most %d", f.Path, len(f.Content), maxAgentFileBytes),
				Remedy:  "shorten what goes into the guest; a certificate bundle with everything in it is the usual cause",
			}
		}
		if err := p.client.AgentFileWrite(ctx, node, vmid, f.Path, f.Content); err != nil {
			return provider.OperationRef{}, err
		}
		if f.Mode == 0 {
			continue
		}
		// file-write has no mode parameter, so the file lands at whatever the
		// guest agent's umask gives it -- world-readable, on a file holding a
		// join token. The chmod goes immediately after each write rather than
		// once at the end, because the window between the two is the only time
		// that credential is readable by anything else on the machine.
		if err := p.runInGuest(ctx, node, vmid, []string{chmodCommand, fmt.Sprintf("%04o", f.Mode), f.Path}); err != nil {
			return provider.OperationRef{}, err
		}
	}

	if len(payload.Commands) == 0 {
		// The files are all there was. There is nothing to follow, and the
		// caller reads that as a bootstrap that is done.
		return provider.OperationRef{}, nil
	}
	for _, argv := range payload.Commands[:len(payload.Commands)-1] {
		if err := p.runInGuest(ctx, node, vmid, argv); err != nil {
			return provider.OperationRef{}, err
		}
	}
	last := payload.Commands[len(payload.Commands)-1]
	pid, err := p.client.AgentExec(ctx, node, vmid, last, "")
	if err != nil {
		return provider.OperationRef{}, err
	}
	return provider.OperationRef{Kind: provider.OpBootstrap, Handle: execHandle(node, vmid, pid)}, nil
}

// awaitGuestAgent waits for the guest to answer, which is how a machine says it
// has booted far enough to be given its enrolment.
//
// It waits rather than reporting the first refusal because the two are
// indistinguishable from outside: a guest thirty seconds into its boot and a
// template with no agent installed both answer "QEMU guest agent is not
// running". The context bounds the wait, and when it runs out the last refusal
// is what is reported -- with the remedy that is nearly always right.
func (p *Provider) awaitGuestAgent(ctx context.Context, node string, vmid int) error {
	timer := time.NewTimer(guestAgentPollInterval)
	defer timer.Stop()
	for {
		err := p.client.AgentPing(ctx, node, vmid)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return p.guestAgentSilent(node, vmid, err)
		}
		timer.Reset(guestAgentPollInterval)
		select {
		case <-timer.C:
		case <-ctx.Done():
			return p.guestAgentSilent(node, vmid, err)
		}
	}
}

// guestAgentSilent is a guest that never answered, said in the terms an
// operator can act on. It is a refusal rather than an unknown outcome: nothing
// was asked of the machine, so nothing can have happened to it.
func (p *Provider) guestAgentSilent(node string, vmid int, cause error) error {
	if errors.Is(cause, context.Canceled) {
		return cause
	}
	return &provider.Error{
		Kind: provider.FailureRefused, Op: "reach the guest agent", Ref: vmRef(node, vmid),
		Message: fmt.Sprintf("VM %d on %s did not answer its guest agent", vmid, node),
		Remedy: "the enrolment is written through the QEMU guest agent: check that the template has qemu-guest-agent installed and enabled, " +
			"and that the machine has agent: enabled=1",
		Cause: cause,
	}
}

// runInGuest runs one command inside the guest and waits for it, turning a
// non-zero exit into the guest's own account of what went wrong.
func (p *Provider) runInGuest(ctx context.Context, node string, vmid int, argv []string) error {
	pid, err := p.client.AgentExec(ctx, node, vmid, argv, "")
	if err != nil {
		return err
	}
	timer := time.NewTimer(guestExecPollInterval)
	defer timer.Stop()
	for {
		status, err := p.execStatus(ctx, node, vmid, pid)
		if err != nil {
			return err
		}
		if status.Done {
			if status.OK {
				return nil
			}
			return &provider.Error{
				Kind: provider.FailureRefused, Op: "run " + argv[0] + " inside a machine", Ref: vmRef(node, vmid),
				Message: status.Detail,
				Remedy:  "the message is the guest's own; what it names is in the template rather than in this fleet",
			}
		}
		timer.Reset(guestExecPollInterval)
		select {
		case <-timer.C:
		case <-ctx.Done():
			return &provider.Error{
				Kind: provider.FailureRefused, Op: "run " + argv[0] + " inside a machine", Ref: vmRef(node, vmid),
				Message: fmt.Sprintf("%s was still running inside VM %d when this call ran out of time", argv[0], vmid),
				Remedy:  "nothing is forced; the machine is looked at again on the next pass",
				Cause:   ctx.Err(),
			}
		}
	}
}

// execStatus reports what a command inside a guest is doing, and is where the
// guest's own standard error becomes the sentence a machine's page carries.
//
// A refusal is answered as not-found rather than as a failure, and deliberately
// so: the guest agent reaps a process the moment its status is read, so a
// handle asked about twice -- a controller that restarted between reading the
// answer and recording it -- is a process the agent has genuinely forgotten.
// Not-found sends the caller to look at the machine, which for a bootstrap
// means asking whether the credential was spent; a failure would have it retry
// a poll that can never succeed again. A cluster that could not be reached is
// not that, and is passed through so the poll is simply tried again.
func (p *Provider) execStatus(ctx context.Context, node string, vmid, pid int) (provider.OperationStatus, error) {
	status, err := p.client.AgentExecStatus(ctx, node, vmid, pid)
	if err != nil {
		switch provider.KindOf(err) {
		case provider.FailureUnreachable, provider.FailureAmbiguous:
			return provider.OperationStatus{}, err
		}
		if errors.Is(err, context.Canceled) {
			return provider.OperationStatus{}, err
		}
		return provider.OperationStatus{}, &provider.Error{
			Kind: provider.FailureNotFound, Op: "read what a command inside a machine did", Ref: vmRef(node, vmid),
			Message: fmt.Sprintf("the guest agent on VM %d no longer knows about process %d", vmid, pid),
			Cause:   err,
		}
	}
	if !bool(status.Exited) {
		return provider.OperationStatus{Detail: "installing the agent inside the machine"}, nil
	}
	if status.ExitCode == 0 && status.Signal == 0 {
		return provider.OperationStatus{Done: true, OK: true, Detail: guestOutput(status)}, nil
	}
	return provider.OperationStatus{Done: true, Detail: guestFailure(status)}, nil
}

// guestFailure is the failure as the guest told it: how it ended, and what it
// wrote. It is shown verbatim, because the guest's own sentence about a missing
// unit is worth more than ours about an exit code.
func guestFailure(status AgentExecStatus) string {
	var b strings.Builder
	if status.Signal != 0 {
		b.WriteString("killed by signal " + strconv.Itoa(status.Signal.Int()))
	} else {
		b.WriteString("exited " + strconv.Itoa(status.ExitCode.Int()))
	}
	if said := guestOutput(status); said != "" {
		b.WriteString(": ")
		b.WriteString(said)
	}
	return b.String()
}

// guestOutput is what the command wrote, standard error first: a program that
// failed says why there, and its standard output is usually the part that
// succeeded.
func guestOutput(status AgentExecStatus) string {
	for _, said := range []string{status.ErrData, status.OutData} {
		if said = strings.TrimSpace(said); said != "" {
			return said
		}
	}
	return ""
}

// execHandle renders a handle for a process inside a guest.
func execHandle(node string, vmid, pid int) string {
	return fmt.Sprintf("%s%s:%d:%d", execHandlePrefix, node, vmid, pid)
}

// parseExecHandle reads one back, reporting whether it was one at all: anything
// else is a cluster task, and Operation asks the cluster about it instead.
func parseExecHandle(handle string) (node string, vmid, pid int, ok bool) {
	rest, found := strings.CutPrefix(handle, execHandlePrefix)
	if !found {
		return "", 0, 0, false
	}
	// The node is first and may contain nothing surprising, so the two numbers
	// are taken from the end -- which is also what keeps a node name with a
	// colon in it from making a handle unreadable.
	rest, pidText, ok := cutLast(rest, ":")
	if !ok {
		return "", 0, 0, false
	}
	node, vmidText, ok := cutLast(rest, ":")
	if !ok || node == "" {
		return "", 0, 0, false
	}
	vmid, err := strconv.Atoi(vmidText)
	if err != nil {
		return "", 0, 0, false
	}
	pid, err = strconv.Atoi(pidText)
	if err != nil {
		return "", 0, 0, false
	}
	return node, vmid, pid, true
}

func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}
