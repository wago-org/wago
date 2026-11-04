//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func preparedCleanupCompile(t *testing.T, kind string, artifact bool) *Compiled {
	t.Helper()

	var params []wasm.ValType
	if kind == "owner-args" {
		params = []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	} else if kind == "host" {
		params = []wasm.ValType{wasm.I32}
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32})))}
	if kind == "writer" {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 1))))
	} else if kind == "host" {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(importEntry("env", "target", 0, 0))))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))))
	body := []byte{0x41, 17, 0x0b}
	exports := [][]byte{wasmtest.ExportEntry("run", 0, 0)}
	if strings.HasPrefix(kind, "owner-") || kind == "host" {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})))
		exports = append(exports, wasmtest.ExportEntry("table", 1, 0))
		body = []byte{0x41, 0, 0xd0, 0x70, 0x26, 0, 0x41, 17, 0x0b}
	}
	if kind == "host" {
		exports[0] = wasmtest.ExportEntry("run", 0, 1)
		body = []byte{0x20, 0, 0x10, 0, 0x0b}
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	if kind == "writer" {
		// The genuine active initializer publishes this mapped writer's own
		// function into the owner's actual exported table.
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	config := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	if kind == "host" && runtime.GOARCH != "riscv64" {
		config = config.WithOptimization("prepared-bounded-entry", false)
	}
	c, err := Compile(config, wasmtest.Module(sections...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	return c

}

func preparedCleanupInstantiate(t *testing.T, c *Compiled, imports *Imports) *Instance {
	t.Helper()
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupTestPanic(func() { _ = in.Close() }) })
	if in.jm == nil || in.ar == nil || in.eng == nil {
		t.Fatal("fixture lacks actual native mappings")
	}
	return in
}

func preparedCleanupSession(t *testing.T, in *Instance) *PreparedSession {
	t.Helper()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	s, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupTestPanic(s.Close) })
	return s
}

func preparedCleanupInvoke(s *PreparedSession, variadic bool) ([]uint64, error) {
	if variadic {
		return s.Invoke(1, 2, 3, 4, 5)
	}
	return s.Invoke0()
}

func preparedCleanupReservationReleased(in *Instance) bool {
	state := in.ensurePluginState()
	return in.invocationState.Load()&instanceInvocationCount == 0 && state.invokeMu.state.Load()&invocationGateHeld == 0 && state.invocationID == 0
}

func preparedCleanupProbe(probe, owner *Instance, table *Table) {
	for _, mu := range []*sync.Mutex{&nativeExecutionMu, &owner.lifeMu, &table.mu, &table.owner.mu} {
		if !mu.TryLock() {
			panic("prepared cleanup callback under native/container/lifetime lock")
		}
		mu.Unlock()
	}
	if owner.refStore != nil {
		if !owner.refStore.mu.TryLock() {
			panic("prepared cleanup callback under reference-store lock")
		}
		owner.refStore.mu.Unlock()
	}
	got, err := probe.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 17 {
		panic("distinct mapped native callback probe failed")
	}
}

func TestPreparedCleanupMappedClose(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, variadic := range []bool{false, true} {
			for _, panics := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-args%t-panic%t", artifact, variadic, panics), func(t *testing.T) {
					kind := "owner-fixed"
					if variadic {
						kind = "owner-args"
					}
					owner := preparedCleanupInstantiate(t, preparedCleanupCompile(t, kind, artifact), NewImports())
					probe := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "numeric", artifact), NewImports())
					probe.markNativeControlShared()
					if owner.table != nil {
						t.Fatal("owner eagerly published a table handle")
					}
					s := preparedCleanupSession(t, owner)
					if s.state.fast || !s.state.guardCalls {
						t.Fatal("mutable local-table session lacks active-call guard")
					}
					if runtime.GOARCH != "riscv64" && owner.syncMode {
						t.Fatal("existing-target local-table regression accidentally uses sync admission")
					}
					table, err := owner.ExportedTable("table")
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { cleanupTestPanic(func() { _ = table.Close() }) })
					writer := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "writer", artifact), NewImports().Table("env", "table", table))
					got, err := writer.Invoke("run")
					if err != nil || len(got) != 1 || got[0] != 17 {
						t.Fatal("actual writer invocation", got, err)
					}
					var first any = new(int)
					if os.Getenv("WAGO_PREPARED_SESSION_CLEANUP_NIL_PANIC") == "1" {
						first = nil // Qualified separately with GODEBUG=panicnil=1.
					}
					later := new(int)
					counts := [2]int{}
					writer.referenceLifetime().afterPhysicalRelease(func() {
						counts[0]++
						preparedCleanupProbe(probe, owner, table)
						if !s.state.active.Load() || owner.invocationState.Load()&instanceInvocationCount != 1 {
							panic("prepared call lost its active lifetime before producer callback")
						}
						_, reentryErr := preparedCleanupInvoke(s, variadic)
						if reentryErr == nil || !strings.Contains(reentryErr.Error(), "already active") || !s.state.active.Load() {
							panic("failed active CAS consumed the outer call")
						}
						s.Close()
						if s.state.closed.Load() || !s.state.closeRequested.Load() {
							panic("callback Close released the active reservation")
						}
						if err := owner.Close(); err != nil || owner.resourcesClosed {
							panic("callback Close finalized a protected instance")
						}
						if panics {
							panic(first)
						}
					})
					owner.referenceLifetime().afterPhysicalRelease(func() {
						counts[1]++
						if !s.state.closed.Load() || !preparedCleanupReservationReleased(owner) {
							panic("pending session Close stranded its reservation")
						}
						if panics {
							panic(later)
						}
					})
					if err := writer.Close(); err != nil || writer.resourcesClosed {
						t.Fatal("logical Close failed to retain the actual published writer", err)
					}
					var callErr error
					value, panicked := cleanupTestPanic(func() { got, callErr = preparedCleanupInvoke(s, variadic) })
					if callErr != nil || panicked != panics || panics && value != first || counts != [2]int{1, 1} {
						t.Fatal("prepared complete cleanup/first panic", value, panicked, callErr, counts)
					}
					if !panics && (len(got) != 1 || got[0] != 17) {
						t.Fatal("actual prepared result", got)
					}
					if s.state.active.Load() || !s.state.closed.Load() || s.state.host {
						t.Fatal("prepared call or cached native lease remains active")
					}
					funcrefReconcileAssertMappedDrained(t, writer)
					funcrefReconcileAssertMappedDrained(t, owner)
					s.Close()
					if counts != [2]int{1, 1} {
						t.Fatal("repeat Close reran a physical callback")
					}
				})
			}
		}
	}
}

func TestPreparedCleanupMappedReuse(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, variadic := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-args%t", artifact, variadic), func(t *testing.T) {
				kind := "owner-fixed"
				if variadic {
					kind = "owner-args"
				}
				owner := preparedCleanupInstantiate(t, preparedCleanupCompile(t, kind, artifact), NewImports())
				probe := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "numeric", artifact), NewImports())
				probe.markNativeControlShared()
				s := preparedCleanupSession(t, owner)
				table, err := owner.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				writer := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "writer", artifact), NewImports().Table("env", "table", table))
				marker, callbacks := new(int), 0
				writer.referenceLifetime().afterPhysicalRelease(func() {
					callbacks++
					preparedCleanupProbe(probe, owner, table)
					if !s.state.active.Load() || s.state.closed.Load() {
						panic("cleanup callback lost its active reservation")
					}
					panic(marker)
				})
				if err := writer.Close(); err != nil || writer.resourcesClosed {
					t.Fatal("actual writer root was not retained", err)
				}
				value, panicked := cleanupTestPanic(func() { _, _ = preparedCleanupInvoke(s, variadic) })
				if !panicked || value != marker || callbacks != 1 || s.state.active.Load() || s.state.closed.Load() {
					t.Fatal("cleanup panic stranded or closed the reusable session", value, panicked, callbacks)
				}
				if owner.invocationState.Load()&instanceInvocationCount != 1 || owner.ensurePluginState().invokeMu.state.Load()&invocationGateHeld == 0 || owner.ensurePluginState().invocationID == 0 {
					t.Fatal("cleanup panic lost the reserved identity")
				}
				for i := 0; i < 3; i++ {
					got, err := preparedCleanupInvoke(s, variadic)
					if err != nil || len(got) != 1 || got[0] != 17 || callbacks != 1 {
						t.Fatal("actual mapped reuse after panic", got, err, callbacks)
					}
				}
				funcrefReconcileAssertMappedDrained(t, writer)
				s.Close()
				if !preparedCleanupReservationReleased(owner) {
					t.Fatal("reused session stranded its reservation")
				}
			})
		}
	}
}

func TestPreparedCleanupNumericReservation(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			in := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "numeric", artifact), NewImports())
			s := preparedCleanupSession(t, in)
			if s.state.guardCalls {
				t.Fatal("pure numeric session gained a reconciliation guard")
			}
			invoke := func() {
				got, err := s.Invoke0()
				if err != nil || len(got) != 1 || got[0] != 17 {
					panic("actual numeric prepared result")
				}
			}
			invoke()
			if n := testing.AllocsPerRun(25, invoke); n != 0 {
				t.Fatal("warm pure numeric prepared allocation", n)
			}
			s.Close()
			if !preparedCleanupReservationReleased(in) {
				t.Fatal("numeric session reservation remains owned")
			}
		})
	}
}

func TestPreparedCleanupHostPublication(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, idle := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-idle%t", artifact, idle), func(t *testing.T) {
				var in *Instance
				calls := 0
				imports := NewImports()
				imports.HostFunc("env", "target", func(v int32) int32 {
					calls++
					if calls == 1 && !idle {
						// Real parked publication migrates native ownership. The
						// local handle also requires completed-call reconciliation.
						if _, err := in.ExportedTable("table"); err != nil {
							panic(HostTrap{Err: err})
						}
					}
					return v + 1
				})
				in = preparedCleanupInstantiate(t, preparedCleanupCompile(t, "host", artifact), imports)
				independent := in.usesIndependentExecution()
				s := preparedCleanupSession(t, in)
				cached := s.state.host
				if s.state.fast || !s.state.guardCalls || !artifact && (!cached || s.state.entry.local == nil) {
					t.Fatal("fixture lacks its genuine cached independent host lease")
				}
				if !s.state.fn.scalarFast || s.state.fn.boundedNumericHost || !in.syncMode || in.gc != nil || !in.hasSingleDirectTypedScalarHost() {
					t.Fatal("host fixture lost its scalar single-host admission shape")
				}
				if cached != independent || cached && s.state.entry.local == nil {
					t.Fatal("host session missed its admitted cached-lease strategy")
				}
				t.Logf("cached host lease=%t; scalar=%t bounded=%t independent=%t; source proves cached drop, artifact preserves admitted route", cached, s.state.fn.scalarFast, s.state.fn.boundedNumericHost, in.usesIndependentExecution())
				localMu := s.state.entry.local
				if idle {
					if _, err := in.ExportedTable("table"); err != nil {
						t.Fatal(err)
					}
					if cached && (!s.state.host || s.state.hostLeaseValid()) || s.state.active.Load() {
						t.Fatal("idle publication failed to revoke cached host admission")
					}
				}
				for i := 0; i < 3; i++ {
					got, err := s.Invoke1(41)
					if err != nil || len(got) != 1 || got[0] != 42 || s.state.host || s.state.active.Load() {
						t.Fatal("actual host call failed to drop the revoked cached lease", got, err)
					}
					if i == 0 && cached && s.state.hostGate.migrated.Load() != !idle {
						t.Fatal("idle stale drop and parked native migration were not distinguished")
					}
					mutexes := []*sync.Mutex{&nativeExecutionMu}
					if localMu != nil {
						mutexes = append(mutexes, localMu)
					}
					for _, mu := range mutexes {
						if !mu.TryLock() {
							t.Fatal("completed host call stranded native ownership")
						}
						mu.Unlock()
					}
				}
				if calls != 3 || in.ensurePluginState().preparedHostGate != nil || s.state.entry.local != nil || s.state.hostCall != nil {
					t.Fatal("cached host admission was not consumed once", calls)
				}
				s.Close()
				if !preparedCleanupReservationReleased(in) {
					t.Fatal("host session retained invocation authority after Close")
				}
			})
		}
	}
}
