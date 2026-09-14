package agent

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

// An explicit gate keeps the first operation in flight without wall-clock
// sleeps. synctest.Wait proves the other goroutines have reached their waits.
type startupBackend struct {
	*fakeBackend
	entered chan string
	finish  chan error
}

func (b *startupBackend) CreateWithResult(ctx context.Context, spec backend.Spec) (backend.CreateResult, error) {
	b.entered <- spec.RunnerID
	if err := <-b.finish; err != nil {
		return backend.CreateResult{}, err
	}
	return b.fakeBackend.CreateWithResult(ctx, spec)
}

func (b *startupBackend) PrewarmImage(ctx context.Context, image string, policy store.PullPolicy) (string, error) {
	b.entered <- image
	if err := <-b.finish; err != nil {
		return "", err
	}
	return b.fakeBackend.PrewarmImage(ctx, image, policy)
}

func startupAgent(t *testing.T) (*Agent, *fakeTransport, *startupBackend) {
	t.Helper()
	a, tr, be, _ := newAgent(t, 4)
	b := &startupBackend{fakeBackend: be, entered: make(chan string, 8), finish: make(chan error, 8)}
	a.opts.Backends = backend.NewRegistry(b)
	return a, tr, b
}

func startupEntered(t *testing.T, b *startupBackend, want string) {
	t.Helper()
	select {
	case got := <-b.entered:
		if got != want {
			t.Fatalf("started %q, want %q", got, want)
		}
	default:
		t.Fatalf("%q did not start", want)
	}
}

func startupBlocked(t *testing.T, b *startupBackend) {
	t.Helper()
	select {
	case got := <-b.entered:
		t.Fatalf("%q started while this host's startup slot was occupied", got)
	default:
	}
}

func TestRunnerStartsAreSerialAcrossPoolsAndReleaseAfterFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, tr, b := startupAgent(t)
		ctx := context.Background()
		a.dispatch(ctx, createTask("first", "run-first"))
		synctest.Wait()
		startupEntered(t, b, "run-first")

		second := createTask("second", "run-second")
		second.Spec.PoolID = "another-pool"
		a.dispatch(ctx, second)
		synctest.Wait()
		startupBlocked(t, b)
		b.finish <- errors.New("daemon refused the first start")
		synctest.Wait()
		startupEntered(t, b, "run-second")
		if res := <-tr.results; res.OK {
			t.Fatal("the first failure was not reported")
		}
		b.finish <- nil
		synctest.Wait()
		if res := <-tr.results; !res.OK {
			t.Fatalf("the next start failed: %+v", res)
		}
		a.tasks.Wait()
	})
}

func TestPrewarmingSharesTheStartupSlotButRemovalDoesNot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, tr, b := startupAgent(t)
		ctx := context.Background()
		a.dispatch(ctx, Task{
			ID: "warm", Kind: TaskPrewarmImage, PoolID: "pool",
			Backend: store.BackendDocker, Image: "runner-image", PullPolicy: store.PullIfNotPresent,
		})
		synctest.Wait()
		startupEntered(t, b, "runner-image")
		a.dispatch(ctx, createTask("create", "new-runner"))
		a.dispatch(ctx, Task{ID: "remove", Kind: TaskRemoveRunner, RunnerID: "old-runner"})
		synctest.Wait()
		startupBlocked(t, b)
		select {
		case res := <-tr.results:
			if res.TaskID != "remove" || !res.OK {
				t.Fatalf("removal did not bypass startup: %+v", res)
			}
		default:
			t.Fatal("removal was blocked behind startup")
		}
		b.finish <- nil
		synctest.Wait()
		startupEntered(t, b, "new-runner")
		b.finish <- nil
		synctest.Wait()
		a.tasks.Wait()
	})
}

func TestDifferentHostsCanStartTogether(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _, first := startupAgent(t)
		other, _, second := startupAgent(t)
		a.dispatch(context.Background(), createTask("first", "run-first"))
		other.dispatch(context.Background(), createTask("second", "run-second"))
		synctest.Wait()
		startupEntered(t, first, "run-first")
		startupEntered(t, second, "run-second")
		first.finish <- nil
		second.finish <- nil
		synctest.Wait()
		a.tasks.Wait()
		other.tasks.Wait()
	})
}

func TestShutdownCancelsWaitingStartsWithoutCallingTheBackend(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, tr, b := startupAgent(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		a.dispatch(ctx, createTask("first", "run-first"))
		synctest.Wait()
		startupEntered(t, b, "run-first")
		a.dispatch(ctx, createTask("waiting", "run-waiting"))
		synctest.Wait()
		cancel()
		synctest.Wait()
		startupBlocked(t, b)
		if res := <-tr.results; res.TaskID != "waiting" || res.OK {
			t.Fatalf("waiting start was not cancelled: %+v", res)
		}
		b.finish <- nil
		synctest.Wait()
		if res := <-tr.results; res.TaskID != "first" || !res.OK {
			t.Fatalf("already-started work did not finish: %+v", res)
		}
		a.tasks.Wait()
	})
}

func TestQueuedCreateHonoursRemovalAndCordonBeforeStarting(t *testing.T) {
	for _, mode := range []string{"remove", "cordon"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, tr, b := startupAgent(t)
				ctx := context.Background()
				a.dispatch(ctx, createTask("first", "run-first"))
				synctest.Wait()
				startupEntered(t, b, "run-first")
				a.dispatch(ctx, createTask("waiting", "run-waiting"))
				synctest.Wait()
				if mode == "remove" {
					a.dispatch(ctx, Task{ID: "remove", Kind: TaskRemoveRunner, RunnerID: "run-waiting"})
				} else {
					a.mu.Lock()
					a.cordoned = true
					a.mu.Unlock()
				}
				b.finish <- nil
				synctest.Wait()
				startupBlocked(t, b)
				a.tasks.Wait()
				seen := map[string]TaskResult{}
				for len(tr.results) > 0 {
					res := <-tr.results
					seen[res.TaskID] = res
				}
				res, ok := seen["waiting"]
				if !ok || res.OK != (mode == "remove") {
					t.Fatalf("unexpected queued create result: %+v", seen)
				}
				if mode == "remove" && !seen["remove"].OK {
					t.Fatalf("queued removal did not finish: %+v", seen)
				}
			})
		})
	}
}
