package controller

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/gateway"
	"tailscale.com/tstest/integration"
)

// echoTarget is a stand-in for the hypervisor's API: it repeats a byte, which
// is all a dial through the gateway needs to prove a connection was carried.
func echoTarget(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().String()
}

// A gateway restarts on its host whenever the machine's service manager
// decides it should, and keeps the same persisted address when it does. The
// controller's long-lived client cannot re-announce itself once its first
// introduction has been acknowledged, so the dialer must notice the dead
// tunnel and start over from a fresh client: that is what turns "the provider
// lost its connection and needs a controller restart" into "reconnection is
// automatic at both ends".
func TestADialerReachesTheProviderAgainOnceItsGatewayHasRestarted(t *testing.T) {
	shortenDialIntro(t)
	t.Setenv("IN_TS_TEST", "true")
	dm := integration.RunDERPAndSTUN(t, func(string, ...any) {}, "127.0.0.1")
	target := echoTarget(t)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	g, err := gateway.Start(ctx, gateway.Options{Target: target, StateDir: dir, Region: dm.Regions[1]})
	if err != nil {
		t.Fatal(err)
	}
	address := g.Address()
	d, err := newPrivateDialer(address)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// First dial: a fresh introduction, as on the day the provider enrols.
	conn, err := d.DialContext(ctx, "tcp", "127.0.0.1:8006")
	if err != nil {
		t.Fatalf("the first dial through the gateway failed: %v", err)
	}
	_, _ = conn.Write([]byte("hello"))
	if _, err := conn.Read(make([]byte, 5)); err != nil {
		t.Fatalf("nothing came back through the tunnel: %v", err)
	}
	conn.Close()

	// The restart the provider machine performs by itself: same state
	// directory, hence the same address, hence a provider row that still
	// points at it -- and a server that has forgotten who was ever there.
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := gateway.Start(ctx, gateway.Options{Target: target, StateDir: dir, Region: dm.Regions[1]})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.Address() != address {
		t.Fatal("restarting the gateway changed its address")
	}

	conn, err = d.DialContext(ctx, "tcp", "127.0.0.1:8006")
	if err != nil {
		t.Fatalf("the same dialer could not reach the provider again after its gateway restarted: %v", err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("again!"))
	got := make([]byte, 6)
	deadline := time.Now().Add(10 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("the re-established connection carried nothing through: %v", err)
	}
	if string(got) != "again!" {
		t.Fatalf("got %q", got)
	}
}

// A dial that fails says the gateway is not answering, in the sentence that
// says what to check, and never quotes the address it failed against: the
// address is the capability, and the check happens on another machine.
func TestAFailedDialNamesTheGatewayAndKeepsTheAddressOut(t *testing.T) {
	shortenDialIntro(t)
	t.Setenv("IN_TS_TEST", "true")
	dm := integration.RunDERPAndSTUN(t, func(string, ...any) {}, "127.0.0.1")
	// A gateway that starts, tells us its address, and dies before the dial:
	// the machine the gateway runs on is, as far as the controller can tell,
	// switched off.
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	g, err := gateway.Start(ctx, gateway.Options{Target: echoTarget(t), StateDir: dir, Region: dm.Regions[1]})
	if err != nil {
		t.Fatal(err)
	}
	address := g.Address()
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := newPrivateDialer(address)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	dialCtx, dialCancel := context.WithTimeout(ctx, 20*time.Second)
	defer dialCancel()
	dialDone := make(chan error, 1)
	go func() {
		conn, err := d.DialContext(dialCtx, "tcp", "127.0.0.1:8006")
		if conn != nil {
			conn.Close()
		}
		dialDone <- err
	}()
	select {
	case err := <-dialDone:
		if err == nil {
			t.Fatal("a dial against a stopped gateway succeeded")
		}
		for _, leaked := range []string{address, "tc-"} {
			if strings.Contains(err.Error(), leaked) {
				t.Fatalf("the address leaked into the error: %s", err)
			}
		}
		if !strings.Contains(err.Error(), "cannot reach the gateway") {
			t.Fatalf("the error does not say what to check: %s", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("the dial against a stopped gateway did not fail within its deadline")
	}
}

// shortenDialIntro lets a test that makes a dial hang on purpose notice it in a
// second and a half rather than five. A healthy dial through a local tunnel is
// far quicker, so the first attempt still succeeds where it should.
func shortenDialIntro(t *testing.T) {
	t.Helper()
	original := dialIntroTimeout
	dialIntroTimeout = 1500 * time.Millisecond
	t.Cleanup(func() { dialIntroTimeout = original })
}
