//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// This is runtime attachment/lifetime qualification using genuine published
// ordinary function frames and symbolic sites. These tests never execute those
// symbolic sites or claim public dynamic source/artifact execution admission.
func rv64GenerationPlanForTest(t *testing.T, in *Instance, vector bool) []encoder.NativeTailPlan {
	t.Helper()
	frame, err := railshot.NativeFrameBytes(in.c.code, in.c.Entry[0])
	if err != nil {
		t.Fatal(err)
	}
	stride := 8
	if vector {
		stride = 16
	}
	return []encoder.NativeTailPlan{{Kind: encoder.NativeTailIndirect, FunctionIndex: 0, TypeIndex: 1, SourcePC: 1, FrameBytes: frame, SourceBase: 48, SourceStride: stride, ReferenceBase: frame - 8}}
}

func rv64GenerationLockedForTest(f func() error) error {
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	return f()
}

func TestRV64PrivateTailGenerationOverlappingRoots(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-artifact%t-shared%t", vector, artifact, shared), func(t *testing.T) {
					a, producer, tableA, compiled := rv64TableTargetFixture(t, vector, artifact, shared)
					b, err := Instantiate(compiled, a.Imports())
					if err != nil {
						t.Fatal(err)
					}
					defer b.Close()
					tableB, err := b.ExportedTable("table")
					if err != nil {
						t.Fatal(err)
					}
					nativeExecutionMu.Lock()
					copy(tableB.desc[8:8+coreruntime.TableEntryBytes], producer.funcRefDescs[80:80+coreruntime.TableEntryBytes])
					nativeExecutionMu.Unlock()
					if !tableA.retainProducerInstance(producer) || !tableB.retainProducerInstance(producer) {
						t.Fatal("particular table roots")
					}
					var scopeA, scopeB rv64NativeTableTargets
					var generations []*rv64NativeTailGeneration
					defer func() {
						nativeExecutionMu.Lock()
						for _, g := range generations {
							if err := g.Close(); err != nil {
								t.Error(err)
							}
						}
						nativeExecutionMu.Unlock()
						scopeA.release()
						scopeB.release()
					}()
					for _, pair := range []struct {
						scope *rv64NativeTableTargets
						root  *Instance
					}{{&scopeA, a}, {&scopeB, b}} {
						if err := rv64TableTargetRefresh(pair.scope, pair.root); err != nil {
							t.Fatal(err)
						}
					}
					plans := rv64GenerationPlanForTest(t, producer, vector)
					limits := rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes}
					create := func(scope *rv64NativeTableTargets) *rv64NativeTailGeneration {
						t.Helper()
						var g *rv64NativeTailGeneration
						err := rv64GenerationLockedForTest(func() error {
							var err error
							g, err = rv64NewDefinedTailGeneration(producer, scope, plans, limits)
							return err
						})
						if g != nil {
							generations = append(generations, g)
						}
						if err != nil {
							t.Fatal("construct detached graph generation", err)
						}
						return g
					}
					activate := func(gs ...*rv64NativeTailGeneration) error {
						return rv64GenerationLockedForTest(func() error { return rv64ActivateNativeTailGenerations(gs) })
					}
					binding := rv64NativeInvocationContext(producer)[abi.RV64TailCapabilityOffset:]
					ga := create(&scopeA)
					if binary.LittleEndian.Uint64(binding) != 0 {
						t.Fatal("staging published a callable binding")
					}
					if err := activate(ga); err != nil {
						t.Fatal(err)
					}
					capA := binary.LittleEndian.Uint64(binding)
					gb := create(&scopeB)
					if capA == 0 || binary.LittleEndian.Uint64(binding) != capA {
						t.Fatal("nested construction disturbed suspended binding")
					}
					if err := activate(gb); err != nil {
						t.Fatal(err)
					}
					capB := binary.LittleEndian.Uint64(binding)
					if capB == 0 || capA == capB {
						t.Fatal("generation capabilities alias")
					}
					if err := activate(ga, nil); !errors.Is(err, railshot.ErrUnsupported) || binary.LittleEndian.Uint64(binding) != capB {
						t.Fatal("partial set validation changed binding", err)
					}
					if err := activate(ga, gb); !errors.Is(err, railshot.ErrUnsupported) || binary.LittleEndian.Uint64(binding) != capB {
						t.Fatal("ambiguous set changed binding", err)
					}
					row := producer.funcRefDescs[88:96]
					key := binary.LittleEndian.Uint64(row)
					binary.LittleEndian.PutUint64(row, key^8)
					if err := activate(ga); err == nil || binary.LittleEndian.Uint64(binding) != capB {
						t.Fatal("failed owner refresh changed active generation", err)
					}
					binary.LittleEndian.PutUint64(row, key)
					if allocations := testing.AllocsPerRun(4, func() {
						for _, g := range []*rv64NativeTailGeneration{ga, gb, ga, gb} {
							if err := activate(g); err != nil {
								panic(err)
							}
						}
					}); allocations != 0 {
						t.Fatal("warm overlapping graph switches allocated", allocations)
					}
					if err := rv64GenerationLockedForTest(ga.Close); err != nil || binary.LittleEndian.Uint64(binding) != capB || scopeA.tailBudget.count != 0 {
						t.Fatal("departing outer root cleared another generation", err)
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if err := compiled.Close(); err != nil {
						t.Fatal(err)
					}
					for _, root := range []*Instance{a, b} {
						if _, err := root.Invoke("clear", 0); err != nil {
							t.Fatal(err)
						}
					}
					scopeA.release()
					if producer.resourcesClosed || activate(gb) != nil {
						t.Fatal("logical close/replaced rows lost suspended producer")
					}
					// This checkpoint retains a fixed graph. Newly attached owners
					// require a separately budgeted generation before any resume.
					nativeExecutionMu.Lock()
					copy(tableB.desc[8:8+coreruntime.TableEntryBytes], a.funcRefDescs[80:80+coreruntime.TableEntryBytes])
					nativeExecutionMu.Unlock()
					if !tableB.retainProducerInstance(a) {
						t.Fatal("new attached root")
					}
					if err := rv64TableTargetRefresh(&scopeB, b); err != nil {
						t.Fatal(err)
					}
					if !errors.Is(activate(gb), railshot.ErrUnsupported) || binary.LittleEndian.Uint64(binding) != capB {
						t.Fatal("fixed generation accepted graph growth or changed binding")
					}
					if err := rv64GenerationLockedForTest(gb.Close); err != nil || binary.LittleEndian.Uint64(binding) != 0 || scopeB.tailBudget != (rv64NativeTailGenerationBudget{}) || gb.state.tailGenerationBudget != (rv64NativeTailGenerationBudget{}) {
						t.Fatal("cold close failed to release generation ownership", err)
					}
					scopeB.release()
					if !producer.resourcesClosed || !errors.Is(activate(gb), railshot.ErrUnsupported) {
						t.Fatal("retired scope was rebound")
					}
				})
			}
		}
	}
}

func TestRV64PrivateTailGenerationAggregateLimits(t *testing.T) {
	root, producer, table, _ := rv64TableTargetFixture(t, false, false, false)
	if !table.retainProducerInstance(producer) {
		t.Fatal("producer root")
	}
	var scope rv64NativeTableTargets
	var generations []*rv64NativeTailGeneration
	defer func() {
		nativeExecutionMu.Lock()
		for _, g := range generations {
			if err := g.Close(); err != nil {
				t.Error(err)
			}
		}
		nativeExecutionMu.Unlock()
		scope.release()
	}()
	if err := rv64TableTargetRefresh(&scope, root); err != nil {
		t.Fatal(err)
	}
	plans := rv64GenerationPlanForTest(t, producer, false)
	construct := func(limits rv64NativeTailGenerationLimits, input []encoder.NativeTailPlan) (*rv64NativeTailGeneration, error) {
		nativeExecutionMu.Lock()
		defer nativeExecutionMu.Unlock()
		g, err := rv64NewDefinedTailGeneration(producer, &scope, input, limits)
		if g != nil {
			generations = append(generations, g)
		}
		return g, err
	}
	for _, owner := range scope.owners[:scope.count] {
		if owner.ensurePluginState().railshotInvocation.definedTailOwner != nil {
			t.Fatal("fixture already has cached owner snapshot")
		}
	}
	if rejected, err := construct(rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, 1}, plans); rejected != nil || !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("cold below-quota construction", err)
	}
	for _, owner := range scope.owners[:scope.count] {
		if owner.ensurePluginState().railshotInvocation.definedTailOwner != nil {
			t.Fatal("rejected quota populated owner snapshot")
		}
	}
	g, err := construct(rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes}, plans)
	if err != nil {
		t.Fatal(err)
	}
	exact := rv64NativeTailGenerationLimits{g.codeBytes, g.metadataBytes}
	if err := rv64GenerationLockedForTest(g.Close); err != nil {
		t.Fatal(err)
	}
	for _, limits := range []rv64NativeTailGenerationLimits{{exact.code - 1, exact.metadata}, {exact.code, exact.metadata - 1}} {
		if rejected, err := construct(limits, plans); rejected != nil || !errors.Is(err, railshot.ErrUnsupported) || scope.tailBudget != (rv64NativeTailGenerationBudget{}) {
			t.Fatal("below exact charge allocated a generation", err)
		}
	}
	g, err = construct(exact, plans)
	if err != nil {
		t.Fatal("exact bound", err)
	}
	var otherScope rv64NativeTableTargets
	if err := rv64TableTargetRefresh(&otherScope, root); err != nil {
		t.Fatal(err)
	}
	defer otherScope.release()
	if err := rv64GenerationLockedForTest(func() error {
		rejected, err := rv64NewDefinedTailGeneration(producer, &otherScope, plans, exact)
		if rejected != nil {
			generations = append(generations, rejected)
			t.Error("cross-scope quota allocated")
		}
		if !errors.Is(err, railshot.ErrUnsupported) {
			t.Error("cross-scope configured aggregate quota", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if otherScope.tailBudget != (rv64NativeTailGenerationBudget{}) {
		t.Fatal("rejected cross-scope quota charged")
	}
	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		scope.release()
	}()
	if !panicked || scope.count != 2 || producer.resourcesClosed {
		t.Fatal("live generation scope roots released")
	}
	before := scope.tailBudget
	if rejected, err := construct(exact, plans); rejected != nil || !errors.Is(err, railshot.ErrUnsupported) || scope.tailBudget != before {
		t.Fatal("live overlapping generations escaped aggregate quota", err)
	}
	bad := append([]encoder.NativeTailPlan(nil), plans...)
	bad[0].FrameBytes += 16
	if rejected, err := construct(rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes}, bad); rejected != nil || !errors.Is(err, railshot.ErrUnsupported) || scope.tailBudget != before {
		t.Fatal("claimed source frame replaced actual native frame", err)
	}
}

func TestRV64PrivateTailGenerationOccupiedSetIsTransactional(t *testing.T) {
	root, producer, table, _ := rv64TableTargetFixture(t, false, false, false)
	if !table.retainProducerInstance(producer) {
		t.Fatal("producer root")
	}
	var scope rv64NativeTableTargets
	var generations []*rv64NativeTailGeneration
	defer func() {
		nativeExecutionMu.Lock()
		binary.LittleEndian.PutUint64(rv64NativeInvocationContext(root)[abi.RV64TailCapabilityOffset:], 0)
		for _, g := range generations {
			if err := g.Close(); err != nil {
				t.Error(err)
			}
		}
		nativeExecutionMu.Unlock()
		scope.release()
	}()
	if err := rv64TableTargetRefresh(&scope, root); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []*Instance{producer, root} {
		plans := rv64GenerationPlanForTest(t, owner, false)
		if err := rv64GenerationLockedForTest(func() error {
			g, err := rv64NewDefinedTailGeneration(owner, &scope, plans, rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes})
			if g != nil {
				generations = append(generations, g)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := rv64GenerationLockedForTest(func() error { return rv64ActivateNativeTailGenerations(generations[:1]) }); err != nil {
		t.Fatal(err)
	}
	first := rv64NativeInvocationContext(producer)[abi.RV64TailCapabilityOffset:]
	second := rv64NativeInvocationContext(root)[abi.RV64TailCapabilityOffset:]
	capability := binary.LittleEndian.Uint64(first)
	budget := scope.tailBudget
	binary.LittleEndian.PutUint64(second, 8)
	err := rv64GenerationLockedForTest(func() error { return rv64ActivateNativeTailGenerations(generations) })
	if !errors.Is(err, railshot.ErrUnsupported) || capability == 0 || binary.LittleEndian.Uint64(first) != capability || binary.LittleEndian.Uint64(second) != 8 || generations[0].state.activeTailGeneration != generations[0] || generations[1].state.activeTailGeneration != nil || scope.tailBudget != budget {
		t.Fatal("later occupied caller partially changed graph", err)
	}
	binary.LittleEndian.PutUint64(second, 0)
	if err := rv64GenerationLockedForTest(func() error { return rv64ActivateNativeTailGenerations(generations) }); err != nil {
		t.Fatal("valid two-caller activation", err)
	}
}

func TestRV64PrivateTailGenerationDistinctImageOverlapQuota(t *testing.T) {
	create := func(name byte) *Instance {
		t.Helper()
		source := append(rv64PublicTableExportModule(false, false), wasmtest.Section(0, []byte{1, name})...)
		c := rv64PublicStartCompile(t, source, false)
		in, err := Instantiate(c, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = in.Close() })
		return in
	}
	producer, a, b := create('p'), create('a'), create('b')
	if producer.base == a.base || producer.base == b.base || a.base == b.base {
		t.Fatal("fixture requires three distinct published images")
	}
	for _, root := range []*Instance{a, b} {
		table, err := root.ExportedTable("table")
		if err != nil {
			t.Fatal(err)
		}
		nativeExecutionMu.Lock()
		copy(table.desc[8:8+coreruntime.TableEntryBytes], producer.funcRefDescs[80:80+coreruntime.TableEntryBytes])
		nativeExecutionMu.Unlock()
		if !table.retainProducerInstance(producer) {
			t.Fatal("producer root")
		}
	}
	var scopeA, scopeB rv64NativeTableTargets
	var generations []*rv64NativeTailGeneration
	defer func() {
		nativeExecutionMu.Lock()
		for _, g := range generations {
			if err := g.Close(); err != nil {
				t.Error(err)
			}
		}
		nativeExecutionMu.Unlock()
		scopeA.release()
		scopeB.release()
	}()
	if err := rv64TableTargetRefresh(&scopeA, a); err != nil {
		t.Fatal(err)
	}
	if err := rv64TableTargetRefresh(&scopeB, b); err != nil {
		t.Fatal(err)
	}
	plans := rv64GenerationPlanForTest(t, producer, false)
	makeGeneration := func(scope *rv64NativeTableTargets, limits rv64NativeTailGenerationLimits) (*rv64NativeTailGeneration, error) {
		nativeExecutionMu.Lock()
		defer nativeExecutionMu.Unlock()
		g, err := rv64NewDefinedTailGeneration(producer, scope, plans, limits)
		if g != nil {
			generations = append(generations, g)
		}
		return g, err
	}
	probe, err := makeGeneration(&scopeA, rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes})
	if err != nil {
		t.Fatal(err)
	}
	guestA, guestB := len(a.c.code)+len(producer.c.code), len(b.c.code)+len(producer.c.code)
	factory := probe.codeBytes - guestA
	if factory <= 0 {
		t.Fatal("factory charge missing guest images")
	}
	if err := rv64GenerationLockedForTest(probe.Close); err != nil {
		t.Fatal(err)
	}
	// The former per-graph check allowed both factories plus only the newest
	// graph's guest images. The first graph's distinct root image is still live.
	limits := rv64NativeTailGenerationLimits{2*factory + max(guestA, guestB), railshot.MaxModuleBytes}
	first, err := makeGeneration(&scopeA, limits)
	if err != nil {
		t.Fatal("first individually fitting graph", err)
	}
	before := first.state.tailGenerationBudget
	if rejected, err := makeGeneration(&scopeB, limits); rejected != nil || !errors.Is(err, railshot.ErrUnsupported) || first.state.tailGenerationBudget != before || scopeB.tailBudget != (rv64NativeTailGenerationBudget{}) {
		t.Fatal("distinct retained guest union escaped caller quota", err)
	}
	if b.ensurePluginState().railshotInvocation.definedTailOwner != nil {
		t.Fatal("cross-scope rejection constructed a target snapshot")
	}
}

func TestRV64PrivateTailGenerationFailedConstructionAndCleanup(t *testing.T) {
	root, producer, table, _ := rv64TableTargetFixture(t, false, false, false)
	if !table.retainProducerInstance(producer) {
		t.Fatal("producer root")
	}
	var scope rv64NativeTableTargets
	var generation *rv64NativeTailGeneration
	defer func() {
		if err := rv64GenerationLockedForTest(func() error { return generation.Close() }); err != nil {
			t.Error(err)
		}
		scope.release()
	}()
	if err := rv64TableTargetRefresh(&scope, root); err != nil {
		t.Fatal(err)
	}
	plans := rv64GenerationPlanForTest(t, producer, false)
	construct := func() error {
		return rv64GenerationLockedForTest(func() error {
			var err error
			generation, err = rv64NewDefinedTailGeneration(producer, &scope, plans, rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes})
			return err
		})
	}
	// Raw fields are first authenticated by snapshot construction, after the
	// count-only reservation. A partial generation must retain cleanup charges.
	row := producer.funcRefDescs[88:96]
	key := binary.LittleEndian.Uint64(row)
	binary.LittleEndian.PutUint64(row, key^8)
	err := construct()
	binary.LittleEndian.PutUint64(row, key)
	if !errors.Is(err, railshot.ErrUnsupported) || generation == nil || generation.factory != nil || !generation.reserved || scope.tailBudget.count != 1 || generation.state.tailGenerationBudget.count != 1 {
		t.Fatal("post-reservation failure lost cleanup ownership", err)
	}
	if err := rv64GenerationLockedForTest(generation.Close); err != nil || scope.tailBudget != (rv64NativeTailGenerationBudget{}) || generation.state.tailGenerationBudget != (rv64NativeTailGenerationBudget{}) {
		t.Fatal("nil-factory cleanup reservation", err)
	}
	if err := construct(); err != nil {
		t.Fatal(err)
	}
	if err := rv64GenerationLockedForTest(func() error { return rv64ActivateNativeTailGenerations([]*rv64NativeTailGeneration{generation}) }); err != nil {
		t.Fatal(err)
	}
	binding := rv64NativeInvocationContext(producer)[abi.RV64TailCapabilityOffset:]
	capability, before := binary.LittleEndian.Uint64(binding), scope.tailBudget
	binary.LittleEndian.PutUint64(binding, 8)
	err = rv64GenerationLockedForTest(generation.Close)
	if !errors.Is(err, railshot.ErrUnsupported) || generation.ready || !generation.reserved || scope.tailBudget != before || generation.state.tailGenerationBudget.count != 1 || binary.LittleEndian.Uint64(binding) != 8 {
		t.Fatal("failed detach released reservation or overwrote binding", err)
	}
	if err := rv64GenerationLockedForTest(func() error { return rv64ActivateNativeTailGenerations([]*rv64NativeTailGeneration{generation}) }); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("failed-close generation resumed", err)
	}
	binary.LittleEndian.PutUint64(binding, capability)
	if err := rv64GenerationLockedForTest(generation.Close); err != nil || binary.LittleEndian.Uint64(binding) != 0 || scope.tailBudget != (rv64NativeTailGenerationBudget{}) || generation.state.tailGenerationBudget != (rv64NativeTailGenerationBudget{}) {
		t.Fatal("retained detach/cleanup retry", err)
	}
}
