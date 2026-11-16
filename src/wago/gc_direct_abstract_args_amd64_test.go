//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"strings"
	"testing"
)

func directAbstractArgumentBytes(heap byte, nullable bool) []byte {
	ref := byte(0x64)
	if nullable {
		ref = 0x63
	}
	types := [][]byte{{0x5f, 1, 0x7f, 0}, {0x5e, 0x7f, 1}, {0x60, 1, 0x7f, 1, 0x63, 0}, {0x60, 1, 0x7f, 1, 0x63, 1}, {0x60, 1, ref, heap, 1, ref, heap}}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3}, []byte{4})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("new", 0, 0), wasmtest.ExportEntry("array", 0, 1), wasmtest.ExportEntry("pass", 0, 2))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x41, 2, 0xfb, 6, 1, 0x0b}), wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x20, 0, 0x0b}))))
}
func TestDirectGCAbstractManagedArguments(t *testing.T) {
	for _, kind := range []struct {
		name                  string
		heap                  byte
		structure, array, i31 bool
	}{
		{"any", 0x6e, true, true, true}, {"eq", 0x6d, true, true, true}, {"struct", 0x6b, true, false, false}, {"array", 0x6a, false, true, false}, {"none", 0x71, false, false, false},
	} {
		for _, nullable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/nullable=%v", kind.name, nullable), func(t *testing.T) {
				c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false).WithOptimization("inline", false), directAbstractArgumentBytes(kind.heap, nullable))
				if e != nil {
					t.Fatal(e)
				}
				defer c.Close()
				if !c.hasGCHelperOnlyReceipt() && c.gcDirectReadABIReceipt() == 0 {
					t.Skip("requires direct/helper compiler artifact")
				}
				rt := NewRuntime()
				defer rt.Close()
				makeIn := func() *Instance {
					in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 128, ThroughputHeapBytes: 4096, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}})
					if e != nil {
						t.Fatal(e)
					}
					t.Cleanup(func() { in.Close() })
					return in
				}
				in := makeIn()
				other := makeIn()
				if !in.gc.UsesDirectOffsets() {
					t.Fatal("wrong collector")
				}
				structure, e := in.Invoke("new", 91)
				if e != nil {
					t.Fatal(e)
				}
				s := structure[0]
				defer in.ReleaseGCRef(GCRef{token: s})
				array, e := in.Invoke("array", 37)
				if e != nil {
					t.Fatal(e)
				}
				a := array[0]
				defer in.ReleaseGCRef(GCRef{token: a})
				for _, v := range []struct {
					name           string
					bits           uint64
					accept, object bool
				}{{"null", 0, nullable, false}, {"i31", uint64(gc.I31New(17)), kind.i31, false}, {"struct", s, kind.structure, true}, {"array", a, kind.array, true}, {"raw", 24, false, false}} {
					t.Run(v.name, func(t *testing.T) {
						var before gc.Ref
						var entry gcRefTokenEntry
						if v.object {
							entry = in.refStore.gcByToken[v.bits]
							before = in.gc.GlobalSlot(entry.slot)
						}
						collections := in.gc.Stats().FullCollections
						out, e := in.Invoke("pass", v.bits)
						if !v.accept {
							if e == nil {
								t.Fatal("unsupported value accepted", out)
							}
						} else {
							if e != nil || len(out) != 1 {
								t.Fatal(out, e)
							}
							if in.gc.Stats().FullCollections <= collections {
								t.Fatal("callee allocation did not collect")
							}
							if v.object {
								moved := in.gc.GlobalSlot(entry.slot)
								if moved == before {
									t.Fatal("argument did not move inside callee")
								}
								returned, ok := in.refStore.gcByToken[out[0]]
								if !ok || in.gc.GlobalSlot(returned.slot) != moved {
									t.Fatal("returned stale or different object")
								}
								if v.name == "struct" {
									value, e := in.gc.StructGet(moved, 0)
									if e != nil || value.Bits != 91 {
										t.Fatal(value, e)
									}
								} else {
									value, e := in.gc.ArrayGet(moved, 0)
									if e != nil || value.Bits != 37 {
										t.Fatal(value, e)
									}
								}
								if out[0] != v.bits {
									if e = in.ReleaseGCRef(GCRef{token: out[0]}); e != nil {
										t.Fatal(e)
									}
								}
							} else if out[0] != v.bits {
								t.Fatal("immediate changed", out)
							}
						}
						if st := in.existingPublicGCState(); st != nil && st.argumentRootCount != 0 {
							t.Fatal("argument roots leaked")
						}
						if e = in.gc.Verify(gc.EmptyRoots{}); e != nil {
							t.Fatal(e)
						}
					})
				}
				producer := "new"
				if kind.name == "array" {
					producer = "array"
				}
				foreign, e := other.Invoke(producer, 5)
				if e != nil {
					t.Fatal(e)
				}
				defer other.ReleaseGCRef(GCRef{token: foreign[0]})
				if _, e = in.Invoke("pass", foreign[0]); e == nil {
					t.Fatal("cross-instance token accepted")
				}
			})
		}
	}
}

func TestDirectAbstractArgumentAdmissionFamilies(t *testing.T) {
	for _, h := range []AbstractHeapType{AbstractHeapExtern, AbstractHeapNoExtern, AbstractHeapFunc, AbstractHeapNoFunc, AbstractHeapExn, AbstractHeapNoExn, AbstractHeapString} {
		if directManagedArgumentType(ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: h}}}, nil) {
			t.Fatal("forbidden family", h)
		}
	}
	for _, h := range []AbstractHeapType{AbstractHeapAny, AbstractHeapEq, AbstractHeapStruct, AbstractHeapArray, AbstractHeapNone} {
		if directManagedArgumentType(ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Exact: true, Heap: HeapTypeDescriptor{Abstract: h}}}, nil) {
			t.Fatal("exact abstract admitted", h)
		}
	}
}

func TestDirectGCRejectsInternalExternConversion(t *testing.T) {
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, []byte{0x60, 1, 0x63, 0x6e, 1, 0x63, 0x6e})), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("roundtrip", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x20, 0, 0xfb, 0x1b, 0xfb, 0x1a, 0x0b}))))
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if !c.stagedGCStructProduct().requiresExternConversion() {
		t.Fatal("fixture missed conversion product")
	}
	rt := NewRuntime()
	defer rt.Close()
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true}})
	if e == nil {
		in.Close()
		t.Fatal("internal conversion admitted")
	}
	if !strings.Contains(e.Error(), "extern conversion products") {
		t.Fatal("fixture did not reach conversion guard", e)
	}
}
