package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/provider"
	"github.com/eyupio/zoomies/internal/store"
)

// What goes inside a guest, and what deliberately does not.
//
// A machine receives exactly one credential: a single-use, minutes-long join
// token scoped to this machine's own name. Its only power is to create one host
// row with labels an operator chose. It cannot read the provider's API,
// enumerate machines or delete anything -- and if it is copied out of the guest
// it enrols nothing, because the store refuses it under any other name inside
// the transaction that would spend it.
//
// The provider's own credential never comes near this. It is sealed in the
// provider row under the instance key, unsealed only for the life of one
// client, and there is no code path that could copy it into a payload:
// provider.Bootstrap has files and argv arrays and no field a credential could
// go in. A test plants a recognisable token in the provider row and scans every
// byte of every payload for it.
//
// Nothing here builds a shell command line. The transport writes bytes and runs
// an argv array, which removes the entire quoting and injection class that the
// human join command's shell round-trip test exists to police.

const (
	// machineEnvPath is where the agent's unit reads its environment from --
	// the path and the key names internal/installer already writes, so a
	// template prepared with the ordinary agent install needs nothing special.
	machineEnvPath = "/etc/zoomies/zoomies.env"
	// machineEnvMode is 0600 because the file holds a join token. Some
	// transports cannot set a mode when they write a file, which is why the
	// chmod below is a command rather than an assumption.
	machineEnvMode = 0o600
	// machineWorkDir is where a runner's checkout and caches land inside the
	// guest.
	machineWorkDir = "/var/lib/zoomies/work"
	// machineCAPath is where a provider-supplied controller CA is written, for
	// a controller behind a certificate the guest's trust store has never seen.
	machineCAPath = "/etc/zoomies/controller-ca.pem"
	// machineTokenGrace is added to the enrolment timeout when the token is
	// minted. The credential must outlast the window the machine is given to
	// use it, or a machine that booted slowly is refused by its own token --
	// and it must not outlast it by much, because nobody is watching it.
	machineTokenGrace = 5 * time.Minute
)

// stepBootstrapping puts the enrolment payload inside the guest.
//
// A bootstrap may have to be run again -- a payload that was written and never
// acknowledged is indistinguishable from one that was lost -- so the first
// question it asks is whether this machine's credential has already been
// spent. A redeemed token is proof the guest got the payload, whatever became
// of the acknowledgement, and re-running would then be writing over an agent
// that is already joining.
func (c *Controller) stepBootstrapping(ctx context.Context, env *machineEnv, pr *machineProvider, m *store.Machine) error {
	if m.OpHandle != "" {
		status, err := pr.p.Operation(ctx, provider.OperationRef{Kind: provider.OpBootstrap, Handle: m.OpHandle})
		switch {
		case errors.Is(err, provider.ErrNotFound):
		case err != nil:
			return err
		case !status.Done:
			return errMachineWaiting
		case !status.OK:
			// The guest's own words, on the bootstrap column rather than the
			// provider one: two systems complained, and an operator reading
			// one error has to know which half to go and look at.
			detail := status.Detail
			if detail == "" {
				detail = "the agent install did not report why it failed"
			}
			if err := c.st.RecordMachineFailure(ctx, m.ID, store.MachineErrorBootstrap, detail,
				c.Now().Add(c.machineBackoff(m.Attempts+1))); err != nil {
				c.log.Warn("could not record a bootstrap failure", "machine", m.ID, "error", err)
			}
			return &provider.Error{Kind: provider.FailureRefused, Op: "bootstrap", Ref: m.Name, Message: detail}
		default:
			c.transitionMachine(ctx, env, m, store.MachineEnrolling,
				"the agent is installed and is joining this controller")
			return nil
		}
	}

	// A token this machine minted that has already been spent means the agent
	// inside the guest has what it needs, whatever became of the payload we
	// wrote: the write is what could be lost, and the redemption is proof it
	// was not.
	if spent, err := c.machineTokenSpent(ctx, m); err != nil {
		c.log.Warn("could not read a machine's join token", "machine", m.ID, "error", err)
	} else if spent {
		c.transitionMachine(ctx, env, m, store.MachineEnrolling,
			"the agent has its credential and is joining this controller")
		return nil
	}

	boot, ok := pr.p.(provider.Bootstrapper)
	if !ok {
		// The payload travelled with the create, so there is nothing to push.
		// The machine is waiting on the agent inside it, which the enrolment
		// timeout bounds.
		c.transitionMachine(ctx, env, m, store.MachineEnrolling,
			"the enrolment payload went with this machine; waiting for its agent to join")
		return nil
	}
	if held := c.mutationsHeld(); held != "" {
		return fmt.Errorf("%w: %s", errMachineHeld, held)
	}
	payload, err := c.machineBootstrap(ctx, env, pr.row, m)
	if err != nil {
		return err
	}
	op, err := boot.Bootstrap(ctx, machineRef(m), payload)
	if err != nil {
		return c.noteAmbiguity(ctx, m, err)
	}
	if op.Zero() {
		c.transitionMachine(ctx, env, m, store.MachineEnrolling,
			"the agent is installed and is joining this controller")
		return nil
	}
	if err := c.st.SetMachineOperationHandle(ctx, m.ID, m.OpID, op.Handle); err != nil {
		c.log.Warn("could not record a bootstrap's handle", "machine", m.ID, "error", err)
	}
	return errMachineWaiting
}

// machineBootstrap mints this machine's credential if it has none and renders
// the payload that carries it.
func (c *Controller) machineBootstrap(ctx context.Context, env *machineEnv, row *store.Provider, m *store.Machine) (provider.Bootstrap, error) {
	token, err := c.machineJoinToken(ctx, env, row, m)
	if err != nil {
		return provider.Bootstrap{}, err
	}
	return bootstrapPayload(m, row, token, env.cfg), nil
}

// machineJoinToken returns the plaintext of this machine's join token, minting
// one when it has none this pass can use.
//
// A token already minted cannot be re-read -- only its hash is stored -- so a
// machine whose payload was written and lost gets a fresh one, and the one it
// replaces is revoked rather than left to expire. Either would be safe, since
// both are single-use and scoped to the same single name; revoking is what
// keeps "one machine, one live credential" true rather than merely harmless.
func (c *Controller) machineJoinToken(ctx context.Context, env *machineEnv, row *store.Provider, m *store.Machine) (string, error) {
	if m.JoinTokenID != "" {
		spent, err := c.machineTokenSpent(ctx, m)
		switch {
		case err != nil:
			return "", err
		case spent:
			// Already redeemed: the agent has what it needs and the machine is
			// waiting to be linked, not to be bootstrapped again.
			return "", fmt.Errorf("%w: machine %s has already spent its join token", errMachineWaiting, m.ID)
		}
		// Unspent, and its plaintext cannot be read back -- only the hash is
		// kept. Re-minting is the only way to write the payload again, and the
		// one it replaces is revoked here rather than left to expire: two live
		// credentials for one machine is one more than the design allows
		// itself, even where both are scoped to the same single name.
		if err := c.st.DeleteJoinToken(ctx, m.JoinTokenID); err != nil && !errors.Is(err, store.ErrNotFound) {
			c.log.Warn("could not revoke a machine's unused join token",
				"machine", m.ID, "token", m.JoinTokenID, "error", err)
		}
	}
	ttl := env.cfg.Provider.EnrolTimeout + machineTokenGrace
	tok, plaintext, err := c.authsvc.CreateScopedJoinToken(ctx, auth.JoinScope{
		TTL:      ttl,
		Labels:   row.MachineLabels,
		Capacity: row.MachineCapacity,
		// The two scope fields. The name is the machine's own, which is what
		// the store checks the claim against.
		MachineID:    m.ID,
		ExpectedName: m.Name,
		CreatedBy:    "machine " + m.ID,
	})
	if err != nil {
		return "", fmt.Errorf("minting machine %s's join token: %w", m.ID, err)
	}
	if err := c.st.SetMachineJoinToken(ctx, m.ID, tok.ID); err != nil {
		return "", fmt.Errorf("recording machine %s's join token: %w", m.ID, err)
	}
	m.JoinTokenID = tok.ID
	return plaintext, nil
}

// machineTokenSpent reports whether the credential this machine was given has
// been redeemed, which is the only proof that the payload reached the guest.
func (c *Controller) machineTokenSpent(ctx context.Context, m *store.Machine) (bool, error) {
	if m.JoinTokenID == "" {
		return false, nil
	}
	tok, err := c.st.GetJoinToken(ctx, m.JoinTokenID)
	if errors.Is(err, store.ErrNotFound) {
		// Pruned or revoked: not proof of anything, and a fresh one is minted.
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return tok.UsedAt != nil, nil
}

// bootstrapPayload renders what goes into the guest. It is pure so that the
// file an operator would find on a machine is something a test can assert on
// byte for byte, rather than something that only exists after a create.
//
// ZOOMIES_AGENT_NAME is pinned to the machine's own name, and that is not
// cosmetic. machine.DefaultHostName derives a name from the hardware and the
// hostname, so identical clones of one template compute the SAME name -- and
// the second one's join is refused with "a host named %q is already enrolled
// here". Every machine a provider builds is a clone of one template, so this is
// the failure every fleet would hit on its second machine.
func bootstrapPayload(m *store.Machine, row *store.Provider, token string, cfg *config.Config) provider.Bootstrap {
	var b strings.Builder
	b.WriteString("# Zoomies -- written by the controller when this machine was created.\n")
	b.WriteString("# It carries one credential: a single-use join token for this machine alone.\n")
	set := func(key, value string) {
		if value == "" {
			return
		}
		b.WriteString(key + "=" + value + "\n")
	}
	set("ZOOMIES_CONTROLLER_URL", controllerURLFor(row, cfg))
	set("ZOOMIES_JOIN_TOKEN", token)
	set("ZOOMIES_AGENT_NAME", m.Name)
	if capacity := row.MachineCapacity; capacity > 0 {
		set("ZOOMIES_AGENT_CAPACITY", strconv.Itoa(capacity))
	}
	set("ZOOMIES_AGENT_BACKEND", string(row.MachineBackend))
	set("ZOOMIES_AGENT_LABELS", machineLabelsFor(row.MachineLabels))
	set("ZOOMIES_WORK_DIR", machineWorkDir)

	files := []provider.File{{Path: machineEnvPath, Mode: machineEnvMode, Content: []byte(b.String())}}
	if row.CAPEM != "" {
		// Only when the provider supplies one: a guest told to trust a file
		// that is not there refuses to start, which is worse than a guest that
		// uses its own trust store.
		files = append(files, provider.File{Path: machineCAPath, Mode: 0o644, Content: []byte(row.CAPEM)})
		files[0].Content = append(files[0].Content, []byte("ZOOMIES_AGENT_CA_FILE="+machineCAPath+"\n")...)
	}

	return provider.Bootstrap{
		Files: files,
		Commands: [][]string{
			// The chmod is required rather than tidy: a transport that writes
			// a file has no way to say what mode it should have, and this file
			// holds a credential.
			{"/bin/chmod", "0600", machineEnvPath},
			// enable --now, so that the unit the template ships disabled comes
			// up here and comes up again after a reboot. A template that
			// shipped it enabled would have every clone try to join before it
			// had an environment file to join with.
			{"/bin/systemctl", "enable", "--now", "zoomies-agent"},
		},
	}
}

// controllerURLFor is how the guest reaches this controller: the provider's own
// answer where it has one -- a machine on a private network usually cannot use
// the URL a browser does -- and the fleet's external URL otherwise.
func controllerURLFor(row *store.Provider, cfg *config.Config) string {
	if u := strings.TrimSpace(row.Settings["controller_url"]); u != "" {
		return strings.TrimRight(u, "/")
	}
	return strings.TrimRight(cfg.Server.ExternalURL, "/")
}
