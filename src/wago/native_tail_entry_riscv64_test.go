//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Attach symbolic sites after the ordinary fixture has been compiled, validated
// and instantiated. These tests exercise real entry/resume ownership hooks but
// do not execute the symbolic sites or bypass public dynamic-source admission.
func rv64EntryGenerationFixture(t *testing.T, vector bool) (*Instance, *Instance, *Table, *Instance) {
	t.Helper()
	root, producer, table, _ := rv64TableTargetFixture(t, vector, false, false)
	if !table.retainProducerInstance(producer) {
		t.Fatal("producer table root")
	}
	replacement, err := Instantiate(root.c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	for _, in := range []*Instance{root, producer, replacement} {
		m := rv64NativeTailModule(in.c)
		rv64StoreNativeTailModule(in.c, &encoder.NativeTailModule{Plans: rv64GenerationPlanForTest(t, in, vector)})
		t.Cleanup(func() { in.c.ensureCompileIndexes().railshotTail.module = m })
	}
	return root, producer, table, replacement
}

func rv64EntryCapability(in *Instance) uint64 {
	return binary.LittleEndian.Uint64(rv64NativeInvocationContext(in)[abi.RV64TailCapabilityOffset:])
}

func TestRV64TailEntryGenerationLeaseResumeAndGrowth(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprint(vector), func(t *testing.T) {
			root, producer, table, replacement := rv64EntryGenerationFixture(t, vector)
			gate := root.lockInvocation(0)
			defer gate.unlockInvocation()
			entry, err := root.beginNativeEntry()
			if err != nil {
				t.Fatal(err)
			}
			locked := true
			defer func() {
				if locked {
					root.unlockNativeEntry(entry)
				}
			}()
			scope := entry.tables.targets
			if len(scope.tailActive) != 2 || scope.tailBudget.count != 2 || rv64EntryCapability(root) == 0 || rv64EntryCapability(producer) == 0 {
				t.Fatal("entry omitted complete owner generation set")
			}
			outer := railshotCurrentNativeTableTargets(root)
			outerCap := rv64EntryCapability(producer)
			inner, err := railshotAcquireNativeTableTargets(root)
			if err != nil {
				t.Fatal(err)
			}
			innerCap := rv64EntryCapability(producer)
			if innerCap == outerCap || railshotCurrentNativeTableTargets(root).targets != inner.targets {
				t.Fatal("nested entry reused parked capability")
			}
			// The explicit saved lease must select its own generations even when
			// another slot is current. Root-only lookup at resume would select inner.
			railshotRefreshNativeTableTargets(outer)
			if rv64EntryCapability(producer) != outerCap {
				t.Fatal("wrong resumed lease")
			}
			railshotRefreshNativeTableTargets(inner)
			if rv64EntryCapability(producer) != innerCap {
				t.Fatal("wrong nested resume")
			}
			if allocations := testing.AllocsPerRun(4, func() {
				railshotRefreshNativeTableTargets(outer)
				railshotRefreshNativeTableTargets(inner)
			}); allocations != 0 {
				t.Fatal("warm resume allocated", allocations)
			}
			if err := railshotRetireNativeTableTargets(inner); err != nil {
				t.Fatal(err)
			}
			// Root release runs outside native locking, as in real entry cleanup.
			if entry.local != nil {
				entry.local.Unlock()
			} else {
				nativeExecutionMu.Unlock()
			}
			railshotReleaseNativeTableTargets(inner)
			if entry.local != nil {
				entry.local.Lock()
			} else {
				nativeExecutionMu.Lock()
			}
			railshotRefreshNativeTableTargets(outer)
			if rv64EntryCapability(producer) != outerCap || gate.railshotInvocation.tableDepth != 1 {
				t.Fatal("nested exit lost outer factory")
			}
			// Same-owner row changes reuse the generation; ownership growth stages
			// a fresh complete set and retains both versions until cold exit.
			copy(table.desc[8:40], replacement.funcRefDescs[80:112])
			if entry.local != nil {
				entry.local.Unlock()
			} else {
				nativeExecutionMu.Unlock()
			}
			retained := table.retainProducerInstance(replacement)
			if entry.local != nil {
				entry.local.Lock()
			} else {
				nativeExecutionMu.Lock()
			}
			if !retained {
				t.Fatal("new producer root")
			}
			// The replaced producer remains leased even after table pruning.
			railshotRefreshNativeTableTargets(outer)
			if len(scope.tailActive) != 3 || len(scope.tailGenerations) != 5 || scope.tailBudget.count != 5 || rv64EntryCapability(producer) == outerCap {
				t.Fatal("growth did not preserve old and bind new complete generations")
			}
			old := scope.tailGenerations[0]
			if !old.reserved || !old.ready || old.factory == nil {
				t.Fatal("parked code retired early")
			}
			locked = false
			root.unlockNativeEntry(entry)
			if scope.count != 0 || scope.tailBudget != (rv64NativeTailGenerationBudget{}) || cap(scope.tailActive) != 0 || cap(scope.tailGenerations) != 0 || rv64EntryCapability(root) != 0 || rv64EntryCapability(producer) != 0 || rv64EntryCapability(replacement) != 0 {
				t.Fatal("cold exit retained generation or binding")
			}
		})
	}
}

func TestRV64TailEntryGenerationFailedDetachQuarantinesAndRetries(t *testing.T) {
	root, producer, _, _ := rv64EntryGenerationFixture(t, false)
	gate := root.lockInvocation(0)
	defer gate.unlockInvocation()
	entry, err := root.beginNativeEntry()
	if err != nil {
		t.Fatal(err)
	}
	scope := entry.tables.targets
	binding := rv64NativeInvocationContext(producer)[abi.RV64TailCapabilityOffset:]
	capability := binary.LittleEndian.Uint64(binding)
	binary.LittleEndian.PutUint64(binding, 8)
	var caught any
	func() {
		defer func() { caught = recover() }()
		root.unlockNativeEntry(entry)
	}()
	if _, ok := caught.(invalidHostReference); !ok || gate.railshotInvocation.tableDepth != 1 || !scope.tailRetiring || scope.count != 2 || scope.tailBudget.count != 1 {
		t.Fatal("failed detach lost exact physical lease", caught)
	}
	// Failed retirement unlocked execution, revoked readiness, and prevents
	// another entry from allocating while the capability remains corrupted.
	before := producer.pluginState.Load().railshotInvocation.tailGenerationBudget
	for n := 0; n < 2; n++ {
		if next, err := root.beginNativeEntry(); !errors.Is(err, railshot.ErrUnsupported) || next.tables.targets != nil || producer.pluginState.Load().railshotInvocation.tailGenerationBudget != before {
			t.Fatal("unrepaired retirement permitted entry", err)
		}
	}
	nativeExecutionMu.Lock()
	binary.LittleEndian.PutUint64(binding, capability)
	nativeExecutionMu.Unlock()
	next, err := root.beginNativeEntry()
	if err != nil {
		t.Fatal("retry repaired lease", err)
	}
	if gate.railshotInvocation.tableDepth != 1 || next.tables.targets.tailRetiring {
		t.Fatal("quarantine slot not reclaimed")
	}
	root.unlockNativeEntry(next)
	if gate.railshotInvocation.tableDepth != 0 || rv64EntryCapability(producer) != 0 {
		t.Fatal("retry cleanup lost ownership")
	}
}

func TestRV64TailEntryGenerationMigratedPreparedRetirement(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprint(prepared), func(t *testing.T) {
			root, producer, _, _ := rv64EntryGenerationFixture(t, false)
			gate := root.lockInvocation(0)
			defer gate.unlockInvocation()
			entry, err := root.beginNativeEntry()
			if err != nil {
				t.Fatal(err)
			}
			if entry.local != nil {
				entry.local.Unlock()
				nativeExecutionMu.Lock()
			}
			if prepared {
				state := preparedSessionState{fn: &WasmFunc{in: root}, entry: entry, host: true}
				state.dropHostLease(true)
			} else {
				// A nonnil original local mutex plus revoked independent mode uses
				// the production migrated unlock branch without unlocking it twice.
				entry.local = &gate.nativeExecutionMu
				root.unlockNativeEntry(entry)
			}
			if gate.railshotInvocation.tableDepth != 0 || rv64EntryCapability(root) != 0 || rv64EntryCapability(producer) != 0 {
				t.Fatal("migrated exit retained factory")
			}
		})
	}
}

func TestRV64TailEntryGenerationDetachedCleanupTransfer(t *testing.T) {
	root, producer, table, _ := rv64TableTargetFixture(t, false, false, false)
	if !table.retainProducerInstance(producer) {
		t.Fatal("producer table root")
	}
	var scope rv64NativeTableTargets
	defer scope.release()
	if err := rv64TableTargetRefresh(&scope, root); err != nil {
		t.Fatal(err)
	}
	var g *rv64NativeTailGeneration
	err := rv64GenerationLockedForTest(func() error {
		var err error
		g, err = rv64NewDefinedTailGeneration(root, &scope, rv64GenerationPlanForTest(t, root, false), rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	code, metadata := g.codeBytes, g.metadataBytes
	factory := g.factory
	err = rv64GenerationLockedForTest(func() error {
		if err := g.factory.Detach(); err != nil {
			return err
		}
		// Release actual mappings, then inject the mapping-close outcome at the
		// exact handoff. Retry tests own no leaked or unmapped caller pointers.
		if err := g.factory.Close(); err != nil {
			return err
		}
		g.finishDetachedRetirement(errors.New("test mapping close failure"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state := &root.c.ensureCompileIndexes().railshotTail
	if len(state.retiredGenerations) != 1 || state.retiredGenerations[0].factory != factory || state.retiredGenerationCode != code || state.retiredGenerationMetadata != metadata || scope.tailBudget != (rv64NativeTailGenerationBudget{}) || g.state.tailGenerationBudget != (rv64NativeTailGenerationBudget{}) || g.reserved || g.factory != nil || len(g.owners) != 0 || len(g.snapshots) != 0 {
		t.Fatal("detached charge/owner transfer")
	}
	cc := root.c.loadCodeCache()
	cc.mu.Lock()
	defer cc.mu.Unlock()
	failure := errors.New("test repeated unmap failure")
	for n := 0; n < 2; n++ {
		if err := rv64RetryRetiredTailGenerations(state, func(*railshot.NativeTailDispatcher) error { return failure }); !errors.Is(err, failure) || len(state.retiredGenerations) != 1 || state.retiredGenerationCode != code || state.retiredGenerationMetadata != metadata {
			t.Fatal("retry forgot owned mapping or charges", err)
		}
	}
	if err := railshotRetryNativeTail(root.c); err != nil || state.retiredGenerations != nil || state.retiredGenerationCode != 0 || state.retiredGenerationMetadata != 0 {
		t.Fatal("successful retry failed to discharge compiled ownership", err)
	}
}

func TestRV64TailEntryGenerationPreparedFailureClearsLease(t *testing.T) {
	for _, migrated := range []bool{false, true} {
		for _, closeSession := range []bool{false, true} {
			t.Run(fmt.Sprintf("migrated%t-close%t", migrated, closeSession), func(t *testing.T) {
				root, producer, _, _ := rv64EntryGenerationFixture(t, false)
				if err := root.beginInvocation(); err != nil {
					t.Fatal(err)
				}
				reserved := root.lockPreparedSessionInvocation()
				entry, err := root.beginNativeEntry()
				if err != nil {
					reserved.unlock()
					root.endInvocation()
					t.Fatal(err)
				}
				if migrated && entry.local != nil {
					entry.local.Unlock()
					nativeExecutionMu.Lock()
				}
				state := preparedSessionState{fn: &WasmFunc{in: root}, lease: reserved, entry: entry, host: true}
				state.hostGate.migrated.Store(migrated)
				plugin := root.ensurePluginState()
				plugin.nativeShareMu.Lock()
				plugin.preparedHostGate = &state.hostGate
				plugin.nativeShareMu.Unlock()
				binding := rv64NativeInvocationContext(producer)[abi.RV64TailCapabilityOffset:]
				capability := binary.LittleEndian.Uint64(binding)
				binary.LittleEndian.PutUint64(binding, 8)
				var caught any
				func() {
					defer func() { caught = recover() }()
					if closeSession {
						state.closeNow()
					} else {
						state.dropHostLease(migrated)
					}
				}()
				if _, ok := caught.(invalidHostReference); !ok || state.host || state.entry.tables.targets != nil || plugin.preparedHostGate != nil {
					t.Fatal("failed retirement left reusable prepared lease", caught)
				}
				// Closing after the failed drop must release the Go reservation
				// exactly once, without repeating the native unlock.
				state.closeNow()
				state.closeNow()
				if root.invocationState.Load()&instanceInvocationCount != 0 || plugin.invokeMu.state.Load()&invocationGateHeld != 0 {
					t.Fatal("prepared reservation remained held")
				}
				nativeExecutionMu.Lock()
				binary.LittleEndian.PutUint64(binding, capability)
				nativeExecutionMu.Unlock()
				plugin.invokeMu.Lock()
				err = railshotRetryNativeTableRetirement(root)
				plugin.invokeMu.Unlock()
				if err != nil || plugin.railshotInvocation.tableDepth != 0 {
					t.Fatal("prepared quarantine retry", err)
				}
			})
		}
	}
}

func TestRV64TailEntryGenerationClosedQuarantineRetry(t *testing.T) {
	root, producer, table, _ := rv64EntryGenerationFixture(t, false)
	if err := root.beginInvocation(); err != nil {
		t.Fatal(err)
	}
	gate := root.lockInvocation(0)
	entry, err := root.beginNativeEntry()
	if err != nil {
		gate.unlockInvocation()
		root.endInvocation()
		t.Fatal(err)
	}
	binding := rv64NativeInvocationContext(producer)[abi.RV64TailCapabilityOffset:]
	capability := binary.LittleEndian.Uint64(binding)
	binary.LittleEndian.PutUint64(binding, 8)
	func() { defer func() { _ = recover() }(); root.unlockNativeEntry(entry) }()
	gate.unlockInvocation()
	root.endInvocation()
	// Remove container retention: only the failed exact scope can now keep the
	// closed producer and its compiled mapping alive through repair.
	if err := table.Close(); err != nil {
		t.Fatal(err)
	}
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := root.c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); !errors.Is(err, railshot.ErrUnsupported) || root.resourcesClosed || producer.resourcesClosed {
		t.Fatal("closed quarantine lost physical roots", err)
	}
	nativeExecutionMu.Lock()
	binary.LittleEndian.PutUint64(binding, capability)
	nativeExecutionMu.Unlock()
	// The original logical close error remains its result, while the quiescent
	// repeated Close performs cleanup without admitting another guest call.
	_ = root.Close()
	if !root.resourcesClosed || !producer.resourcesClosed || gate.railshotInvocation.tableDepth != 0 {
		t.Fatal("closed owner retirement did not retry")
	}
}

func TestRV64TailEntryGenerationClosedRetryDrainsPanickingFinalizers(t *testing.T) {
	root, producer, table, _ := rv64EntryGenerationFixture(t, false)
	if err := root.beginInvocation(); err != nil {
		t.Fatal(err)
	}
	gate := root.lockInvocation(0)
	entry, err := root.beginNativeEntry()
	if err != nil {
		gate.unlockInvocation()
		root.endInvocation()
		t.Fatal(err)
	}
	binding := rv64NativeInvocationContext(producer)[abi.RV64TailCapabilityOffset:]
	capability := binary.LittleEndian.Uint64(binding)
	binary.LittleEndian.PutUint64(binding, 8)
	func() { defer func() { _ = recover() }(); root.unlockNativeEntry(entry) }()
	gate.unlockInvocation()
	root.endInvocation()
	var physical [2]int
	producer.referenceLifetime().afterPhysicalRelease(func() {
		physical[0]++
		// A close from the physical cleanup joins promptly instead of waiting
		// for the same invocation gate that owns this exact retirement.
		_ = root.Close()
		panic("producer physical callback")
	})
	root.referenceLifetime().afterPhysicalRelease(func() { physical[1]++ })
	if err := table.Close(); err != nil {
		t.Fatal(err)
	}
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	nativeExecutionMu.Lock()
	binary.LittleEndian.PutUint64(binding, capability)
	nativeExecutionMu.Unlock()
	if err := root.Close(); !errors.Is(err, ErrCallbackPanic) {
		t.Fatal("close lost callback panic", err)
	}
	if !root.closeState.Load().completed.Load() || !root.resourcesClosed || !producer.resourcesClosed || physical != [2]int{1, 1} || gate.railshotInvocation.tableDepth != 0 || entry.tables.targets.count != 0 || entry.tables.targets.tailRetiring {
		t.Fatal("panic interrupted exact-scope drain or close completion", physical, root.closeState.Load().completed.Load(), root.resourcesClosed, producer.resourcesClosed, gate.railshotInvocation.tableDepth, entry.tables.targets.count, entry.tables.targets.tailRetiring, root.resourceRefs)
	}
	_ = root.Close()
	if physical != [2]int{1, 1} {
		t.Fatal("consumed callbacks ran twice")
	}
}

func TestRV64TailEntryGenerationPartialGrowthFailurePreservesBindings(t *testing.T) {
	root, producer, table, replacement := rv64EntryGenerationFixture(t, false)
	gate := root.lockInvocation(0)
	defer gate.unlockInvocation()
	entry, err := root.beginNativeEntry()
	if err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			root.unlockNativeEntry(entry)
		}
	}()
	scope := entry.tables.targets
	rootCap, producerCap := rv64EntryCapability(root), rv64EntryCapability(producer)
	first, second := scope.tailActive[0], scope.tailActive[1]
	copy(table.desc[8:40], replacement.funcRefDescs[80:112])
	if entry.local != nil {
		entry.local.Unlock()
	} else {
		nativeExecutionMu.Unlock()
	}
	retained := table.retainProducerInstance(replacement)
	if entry.local != nil {
		entry.local.Lock()
	} else {
		nativeExecutionMu.Lock()
	}
	if !retained {
		t.Fatal("replacement root")
	}
	if err := scope.refresh(root); err != nil {
		t.Fatal(err)
	}
	// Corrupt only the second caller's source-plan metadata after its ordinary
	// validation. Target snapshots still authenticate genuine native rows,
	// so the first replacement factory finishes before the second caller fails
	// actual-frame validation. This exercises partial construction ownership.
	original := producer.c
	bad := *original
	indexes := &compiledCacheIndexes{}
	indexes.railshotTail.mappedCodeBytes = original.loadCompileIndexes().railshotTail.mappedCodeBytes
	indexes.railshotTail.module = &encoder.NativeTailModule{Plans: append([]encoder.NativeTailPlan(nil), rv64NativeTailModule(original).Plans...)}
	indexes.railshotTail.module.Plans[0].FrameBytes += 16
	bad.validateMemo = &validateMemo{compileIndexes: indexes}
	producer.c = &bad
	err = scope.refreshTailGenerations()
	producer.c = original
	if !errors.Is(err, railshot.ErrUnsupported) || len(scope.tailGenerations) != 3 || scope.tailBudget.count != 3 || !scope.tailGenerations[2].ready || !scope.tailGenerations[2].reserved {
		t.Fatal("growth failure did not retain its completed partial candidate", err, len(scope.tailGenerations), scope.tailBudget.count)
	}
	if len(scope.tailActive) != 2 || scope.tailActive[0] != first || scope.tailActive[1] != second || rv64EntryCapability(root) != rootCap || rv64EntryCapability(producer) != producerCap || rv64EntryCapability(replacement) != 0 {
		t.Fatal("partial graph publication changed existing bindings")
	}
	locked = false
	root.unlockNativeEntry(entry)
	if scope.count != 0 || scope.tailBudget != (rv64NativeTailGenerationBudget{}) || len(scope.tailGenerations) != 0 || rv64EntryCapability(root) != 0 || rv64EntryCapability(producer) != 0 {
		t.Fatal("failed growth leaked candidate or roots")
	}
	for _, in := range []*Instance{root, producer, replacement} {
		if in.pluginState.Load().railshotInvocation.tailGenerationBudget != (rv64NativeTailGenerationBudget{}) {
			t.Fatal("failed growth leaked caller charge")
		}
	}
}
