//go:build !tinygo && !wago_precompiled

package wago

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func resultRollbackRelay(t *testing.T) (*Runtime, *Instance, *Instance, uint64) {
	t.Helper()
	rt := NewRuntime()
	t.Cleanup(func() { rt.Close() })
	p, err := rt.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	producer, err := rt.Instantiate(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { producer.Close() })
	get, err := producer.ExportedFunc("get")
	if err != nil {
		t.Fatal(err)
	}
	c, err := rt.Compile(funcrefResultReexportModule())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	relay, err := rt.Instantiate(context.Background(), c, WithImports(NewImports().Function("env", "get", get)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { relay.Close() })
	descriptor, ok := producer.localFuncrefDescriptor(0)
	if !ok {
		t.Fatal("producer callable descriptor missing")
	}
	return rt, producer, relay, descriptor
}

func TestPublicReferenceResultRollbackPreservesExactOwnership(t *testing.T) {
	for _, stable := range []bool{false, true} {
		for _, duplicate := range []bool{false, true} {
			t.Run(fmt.Sprintf("stable%t/duplicate%t", stable, duplicate), func(t *testing.T) {
				rt, producer, relay, descriptor := resultRollbackRelay(t)
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				var token uint64
				if stable {
					out, err := relay.Invoke("get")
					if err != nil || len(out) != 1 {
						t.Fatal(out, err)
					}
					token = out[0]
				}
				roots := producer.resourceRefs
				types := []ValType{ValFuncRef}
				exact := []ValueTypeDescriptor{{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}}
				values := []uint64{descriptor}
				if duplicate {
					types = append(types, types[0])
					exact = append(exact, exact[0])
					values = append(values, descriptor)
				}
				types = append(types, ValExternRef)
				exact = append(exact, ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}})
				values = append(values, 0xfeedface)
				err := relay.translatePublicReferenceResultsMode("rollback", values, types, exact, true)
				if err == nil || !strings.Contains(err.Error(), "invalid externref result") {
					t.Fatal("later result was not rejected", err)
				}
				for _, value := range values {
					if value != 0 {
						t.Fatal("failed result was exposed", values)
					}
				}
				if producer.resourceRefs != roots {
					t.Fatalf("producer roots after rollback = %d, want %d", producer.resourceRefs, roots)
				}
				rt.refStore.mu.Lock()
				count := len(rt.refStore.byToken)
				rt.refStore.mu.Unlock()
				want := 0
				if stable {
					want = 1
					if _, ok := rt.refStore.resolve(token); !ok {
						t.Fatal("existing token was revoked")
					}
				}
				if count != want {
					t.Fatalf("tokens after rollback = %d, want %d", count, want)
				}
				if err := relay.Close(); err != nil {
					t.Fatal(err)
				}
				if !stable && producer.hasPhysicalResources() {
					t.Fatal("failed result retained its closed producer")
				}
			})
		}
	}
}

func TestFuncrefResultClaimsAcrossOverlappingBoundaries(t *testing.T) {
	for _, outcome := range []string{"both-fail", "second-succeeds", "direct-issue"} {
		t.Run(outcome, func(t *testing.T) {
			rt, producer, relay, descriptor := resultRollbackRelay(t)
			roots := producer.resourceRefs
			token, first, err := rt.refStore.issueResultMode(relay, descriptor, true, nil, true)
			if err != nil || first == nil {
				t.Fatal(token, first, err)
			}
			token2, second, err := rt.refStore.issueResultMode(relay, descriptor, true, nil, true)
			if err != nil || token != token2 || first != second {
				t.Fatal("overlapping claim", token2, second, err)
			}
			if outcome == "direct-issue" {
				direct, err := rt.refStore.issueAttachedResult(relay, descriptor)
				if err != nil || direct != token {
					t.Fatal(direct, err)
				}
			}
			rt.refStore.finishFuncrefResult(first, false)
			if _, ok := rt.refStore.resolve(token); !ok {
				t.Fatal("first failure revoked second boundary's claim")
			}
			rt.refStore.finishFuncrefResult(second, outcome == "second-succeeds")
			_, live := rt.refStore.resolve(token)
			if live != (outcome != "both-fail") {
				t.Fatal("token publication state", live, outcome)
			}
			want := roots
			if live {
				want++
			}
			if producer.resourceRefs != want {
				t.Fatalf("root count = %d, want %d", producer.resourceRefs, want)
			}
		})
	}
}

func TestPublicFuncrefResultWarmAllocations(t *testing.T) {
	_, _, relay, _ := resultRollbackRelay(t)
	first, err := relay.Invoke("get")
	if err != nil || len(first) != 1 {
		t.Fatal(first, err)
	}
	token := first[0]
	allocations := testing.AllocsPerRun(100, func() {
		out, err := relay.Invoke("get")
		if err != nil || len(out) != 1 || out[0] != token {
			panic("stable funcref result changed")
		}
	})
	t.Logf("warm stable funcref result allocations = %g", allocations)
	if allocations != 0 {
		t.Fatalf("warm stable funcref result allocations = %g, want 0", allocations)
	}
}

func manyDistinctFuncrefResultsModule(count int) []byte {
	results := make([]wasm.ValType, count)
	functions := make([][]byte, count+1)
	bodies := make([][]byte, count+1)
	indices := make([]byte, 0, count*2)
	var get []byte
	for i := range results {
		results[i] = wasm.FuncRef
		functions[i] = []byte{0}
		bodies[i] = wasmtest.Code([]byte{0x0b})
		index := wasmtest.ULEB(uint32(i))
		indices = append(indices, index...)
		get = append(get, 0xd2)
		get = append(get, index...)
	}
	functions[count] = []byte{1}
	bodies[count] = wasmtest.Code(append(get, 0x0b))
	elements := append([]byte{3, 0}, wasmtest.ULEB(uint32(count))...)
	elements = append(elements, indices...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, results))),
		wasmtest.Section(3, wasmtest.Vec(functions...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, uint32(count)))),
		wasmtest.Section(9, wasmtest.Vec(elements)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestPublicManyDistinctFuncrefResultOwnership(t *testing.T) {
	for _, count := range []int{65, 256} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("results%d/fail%t", count, fail), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				c, err := rt.Compile(manyDistinctFuncrefResultsModule(count))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := rt.Instantiate(context.Background(), c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				roots := in.resourceRefs
				if fail {
					values := make([]uint64, count+1)
					types := make([]ValType, len(values))
					exact := make([]ValueTypeDescriptor, len(values))
					for i := 0; i < count; i++ {
						var ok bool
						values[i], ok = in.localFuncrefDescriptor(i)
						if !ok {
							t.Fatal("missing descriptor", i)
						}
						types[i] = ValFuncRef
						exact[i] = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}
					}
					values[count], types[count] = 0xfeedface, ValExternRef
					exact[count] = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}
					if err := in.translatePublicReferenceResults("many results", values, types, exact); err == nil || !strings.Contains(err.Error(), "invalid externref result") {
						t.Fatal("invalid last result accepted", err)
					}
					for _, value := range values {
						if value != 0 {
							t.Fatal("failed result exposed")
						}
					}
					if in.resourceRefs != roots || len(rt.refStore.byToken) != 0 {
						t.Fatal("failed results retained roots or tokens", in.resourceRefs, roots, len(rt.refStore.byToken))
					}
					return
				}
				out, err := in.Invoke("get")
				if err != nil || len(out) != count {
					t.Fatal("large result call", len(out), err)
				}
				seen := make(map[uint64]bool, count)
				for i, token := range out {
					if token == 0 || seen[token] || !in.FuncRefMatchesFunction(ValueOf(ValFuncRef, token).FuncRef(), uint32(i)) {
						t.Fatal("invalid function identity", i, token)
					}
					seen[token] = true
				}
				if in.resourceRefs != roots+count {
					t.Fatal("result root count", in.resourceRefs, roots)
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				if err := rt.CloseContext(context.Background()); err != nil || in.hasPhysicalResources() {
					t.Fatal("runtime close retained result producer", err)
				}
			})
		}
	}
}
