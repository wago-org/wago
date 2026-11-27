//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"testing"
	"time"
)

func TestInstanceCloseTerminalCollectionHandoff(t *testing.T) {
	t.Run("logical-first", func(t *testing.T) {
		state := &instanceCloseState{}
		if _, collect := state.finishLogicalCloseCollectionHandoff(nil, true, nil); collect || !state.completed.Load() {
			t.Fatal("logical owner did not publish before terminal readiness")
		}
		if !state.publishTerminalCollectionReady() || !state.terminalCollectionClaimed || state.terminalCollectionDeferred {
			t.Fatal("terminal owner did not claim the crossed collection")
		}
	})

	t.Run("terminal-first", func(t *testing.T) {
		state := &instanceCloseState{}
		if state.publishTerminalCollectionReady() || state.completed.Load() || !state.terminalCollectionDeferred {
			t.Fatal("terminal owner claimed before logical publication")
		}
		if _, collect := state.finishLogicalCloseCollectionHandoff(nil, false, nil); !collect || state.completed.Load() || !state.terminalCollectionClaimed || state.terminalCollectionDeferred {
			t.Fatal("logical owner did not claim the ready terminal collection")
		}
		state.completeLogicalCloseCollection(nil)
		if !state.completed.Load() {
			t.Fatal("logical result was not published after collection")
		}
	})

	t.Run("simultaneous", func(t *testing.T) {
		type outcome struct {
			collect  bool
			value    any
			panicked bool
		}
		await := func(ch <-chan outcome) outcome {
			t.Helper()
			select {
			case result := <-ch:
				if result.panicked {
					t.Fatalf("handoff worker panicked: %v", result.value)
				}
				return result
			case <-time.After(5 * time.Second):
				t.Fatal("handoff worker did not finish")
				return outcome{}
			}
		}
		for i := 0; i < 100; i++ {
			state := &instanceCloseState{}
			start := make(chan struct{})
			logical := make(chan outcome, 1)
			terminal := make(chan outcome, 1)
			go func() {
				<-start
				var collect bool
				value, panicked := cleanupTestPanic(func() {
					_, collect = state.finishLogicalCloseCollectionHandoff(nil, true, nil)
				})
				logical <- outcome{collect: collect, value: value, panicked: panicked}
			}()
			go func() {
				<-start
				var collect bool
				value, panicked := cleanupTestPanic(func() { collect = state.publishTerminalCollectionReady() })
				terminal <- outcome{collect: collect, value: value, panicked: panicked}
			}()
			close(start)
			logicalCollect, terminalCollect := await(logical).collect, await(terminal).collect
			if logicalCollect == terminalCollect || !state.terminalCollectionClaimed {
				t.Fatalf("crossing claims = logical %t, terminal %t", logicalCollect, terminalCollect)
			}
			if logicalCollect {
				state.completeLogicalCloseCollection(nil)
			}
			if !state.completed.Load() {
				t.Fatal("crossing did not publish logical completion")
			}
		}
	})
}
