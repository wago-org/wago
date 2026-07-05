package wago

import (
	"context"
	"testing"
	"time"
)

// waitFor polls cond until true or the deadline, failing the test on timeout.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSupervisorOneForOne(t *testing.T) {
	rt := NewRuntime()
	class := classFor(t, rt, blockingRecvModule(t))
	defer class.Close()

	sup, err := rt.Supervise(context.Background(),
		SupervisorOptions{Strategy: OneForOne, MaxRestarts: 5, Window: time.Minute},
		ChildSpec{Name: "a", Class: class, Spawn: SpawnOptions{Entry: "run"}},
		ChildSpec{Name: "b", Class: class, Spawn: SpawnOptions{Entry: "run"}},
	)
	if err != nil {
		t.Fatalf("supervise: %v", err)
	}
	defer sup.Stop()

	before := sup.Children()
	if before[0] == 0 || before[1] == 0 {
		t.Fatalf("children not started: %v", before)
	}
	// Kill child a; OneForOne restarts only a.
	if err := rt.Kill(context.Background(), before[0], ExitReason{}); err != nil {
		t.Fatalf("kill: %v", err)
	}
	waitFor(t, "child a restarted", func() bool {
		c := sup.Children()
		return c[0] != 0 && c[0] != before[0]
	})
	if got := sup.Children()[1]; got != before[1] {
		t.Fatalf("child b changed on OneForOne restart: %d -> %d", before[1], got)
	}
}

func TestSupervisorOneForAll(t *testing.T) {
	rt := NewRuntime()
	class := classFor(t, rt, blockingRecvModule(t))
	defer class.Close()

	sup, err := rt.Supervise(context.Background(),
		SupervisorOptions{Strategy: OneForAll, MaxRestarts: 5, Window: time.Minute},
		ChildSpec{Name: "a", Class: class, Spawn: SpawnOptions{Entry: "run"}},
		ChildSpec{Name: "b", Class: class, Spawn: SpawnOptions{Entry: "run"}},
	)
	if err != nil {
		t.Fatalf("supervise: %v", err)
	}
	defer sup.Stop()

	before := sup.Children()
	if err := rt.Kill(context.Background(), before[0], ExitReason{}); err != nil {
		t.Fatalf("kill: %v", err)
	}
	// OneForAll restarts both children with new PIDs.
	waitFor(t, "both children restarted", func() bool {
		c := sup.Children()
		return c[0] != 0 && c[0] != before[0] && c[1] != 0 && c[1] != before[1]
	})
}

func TestSupervisorRestartIntensity(t *testing.T) {
	rt := NewRuntime()
	class := classFor(t, rt, blockingRecvModule(t))
	defer class.Close()

	sup, err := rt.Supervise(context.Background(),
		SupervisorOptions{Strategy: OneForOne, MaxRestarts: 1, Window: time.Hour},
		ChildSpec{Name: "a", Class: class, Spawn: SpawnOptions{Entry: "run"}},
	)
	if err != nil {
		t.Fatalf("supervise: %v", err)
	}
	defer sup.Stop()

	pid0 := sup.Children()[0]
	rt.Kill(context.Background(), pid0, ExitReason{}) // restart #1 (allowed)
	waitFor(t, "first restart", func() bool {
		c := sup.Children()[0]
		return c != 0 && c != pid0
	})
	pid1 := sup.Children()[0]
	rt.Kill(context.Background(), pid1, ExitReason{}) // restart #2 exceeds intensity
	waitFor(t, "supervisor shutdown after intensity exceeded", func() bool {
		return sup.Stopped()
	})
}

func TestSupervisorTransientNoRestartOnNormalExit(t *testing.T) {
	rt := NewRuntime()
	class := classFor(t, rt, blockingRecvModule(t))
	defer class.Close()

	sup, err := rt.Supervise(context.Background(),
		SupervisorOptions{Strategy: OneForOne, MaxRestarts: 5, Window: time.Minute},
		ChildSpec{Name: "a", Class: class, Spawn: SpawnOptions{Entry: "run"}, Restart: RestartTransient},
	)
	if err != nil {
		t.Fatalf("supervise: %v", err)
	}
	defer sup.Stop()

	pid0 := sup.Children()[0]
	// Delivering a message makes recv return and the guest exit normally; a
	// transient child is not restarted on normal exit.
	if err := rt.Send(context.Background(), pid0, []byte("go")); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitFor(t, "child exited", func() bool { return sup.Children()[0] == 0 })
	// It stays down (no restart).
	time.Sleep(50 * time.Millisecond)
	if got := sup.Children()[0]; got != 0 {
		t.Fatalf("transient child restarted on normal exit: pid %d", got)
	}
}
