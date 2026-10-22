//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestRV64RetainedRegistrationMappedProducer(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, callbackPanic := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-panic%t", artifact, callbackPanic), func(t *testing.T) {
				pc := rv64TableDependencyCompile(t, false, artifact)
				producer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				reentry, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer reentry.Close()
				consumer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				rv64TableDependencyInvoke(t, producer)
				if rv64ExistingFunctionDependencyOwnership(producer) != nil {
					t.Fatal("fixture already registered its first producer member")
				}
				if !producer.retainResourceRoot() {
					t.Fatal("actual bootstrap root")
				}
				held := true
				defer func() {
					if held {
						producer.releaseResourceRoot()
					}
				}()
				if err := producer.Close(); err != nil || producer.resourcesClosed || producer.resourceRefs != 1 || producer.jm == nil || producer.ar == nil || producer.eng == nil {
					t.Fatal("actual bootstrap failed to retain mapped storage", err)
				}
				graph, err := newRV64FunctionDependencyGraph(2, 1)
				if err != nil {
					t.Fatal(err)
				}
				member := &rv64FunctionDependencyOwnershipFor(producer).member
				if err := graph.register(member); !errors.Is(err, errDependencyClosed) {
					t.Fatal("ordinary registration reopened actual closed producer", err)
				}
				if err := graph.registerRetained(member); err != nil || producer.resourceRefs != 1 {
					t.Fatal("first retained registration denied actual pinned producer", err)
				}
				identity := member.identity
				if err := graph.registerRetained(member); err != nil || member.identity != identity || producer.resourceRefs != 1 {
					t.Fatal("retained reuse changed actual producer identity", err)
				}
				receipt, err := graph.acquirePending(member, true)
				if err != nil || producer.resourceRefs != 2 {
					t.Fatal("actual guard-to-receipt acquisition", err)
				}
				construction := false
				// The owned receipt drains before construction/consumer cleanup
				// on every assertion exit, including callback-panic fixtures.
				defer func() {
					value, panicked := cleanupTestPanic(func() { receipt.release() })
					if construction {
						consumer.endConstruction()
					}
					if panicked {
						t.Errorf("deferred actual receipt cleanup panicked: %v", value)
					}
				}()
				consumer.beginConstruction(nil)
				construction = true
				destination, err := rv64RegisteredFunctionDependencyMember(graph, consumer)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = receipt.adopt(destination, dependencyFrozenDescriptor); err != nil {
					t.Fatal("actual retained receipt adoption", err)
				}
				producer.releaseResourceRoot()
				held = false
				if producer.resourceRefs != 1 || producer.resourcesClosed || destination.outgoing.Load() != 1 {
					t.Fatal("bootstrap exchange released actual producer storage")
				}
				callbacks, unlocked, reentered := 0, false, false
				marker := new(int)
				producer.referenceLifetime().afterPhysicalRelease(func() {
					callbacks++
					unlocked = nativeExecutionMu.TryLock()
					if unlocked {
						nativeExecutionMu.Unlock()
					}
					if !graph.mu.TryLock() {
						unlocked = false
					} else {
						graph.mu.Unlock()
					}
					if !producer.lifeMu.TryLock() {
						unlocked = false
					} else {
						producer.lifeMu.Unlock()
					}
					if unlocked {
						got, err := reentry.Invoke("run")
						reentered = err == nil && slices.Equal(got, []uint64{17})
					}
					if callbackPanic {
						panic(marker)
					}
				})
				released := false
				value, panicked := cleanupTestPanic(func() { released = receipt.release() })
				if panicked != callbackPanic || callbackPanic && value != marker || !callbackPanic && !released || callbacks != 1 || !unlocked || !reentered || !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil || producer.nativeContext != 0 || graph.live != 0 || destination.outgoing.Load() != 0 {
					t.Fatal("actual retained producer cleanup failed or held locks across native reentry", value, panicked, callbacks, unlocked, reentered)
				}
				if receipt.release() || callbacks != 1 {
					t.Fatal("retained receipt alias repeated actual physical cleanup")
				}
				if err := graph.registerRetained(member); err == nil || member.active.Load() {
					t.Fatal("retained registration reopened retired actual producer", err)
				}
				consumer.endConstruction()
				construction = false
				if err := consumer.Close(); err != nil || len(graph.owners) != 0 {
					t.Fatal("actual retained registration left consumer authority", err)
				}
			})
		}
	}
}
