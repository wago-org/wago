//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"strings"
	"testing"
	"unsafe"
)

func TestGCOptionViewAllocationModes(t *testing.T) {
	modes := []struct {
		name   string
		gc     GCConfig
		native bool
	}{
		{"native", GCConfig{NurseryBytes: 1 << 20, ThroughputHeapBytes: 16 << 20}, true},
		{"go", GCConfig{NurseryBytes: 1 << 20, ThroughputHeapBytes: 16 << 20}, false},
		{"collect", GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 256, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 16 << 20}, true},
		{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}, true},
	}
	for _, mode := range modes {
		for _, n := range []uint32{0, 1, 16} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/%t", mode.name, n, enabled), func(t *testing.T) {
					gcOptionTestSwitch(t, map[bool]string{false: "0", true: "1"}[enabled])
					source, some := iteratorCarrierFixture(n, 130, 3)
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1).WithOptimization("gc-native-alloc", mode.native)
					rt := NewRuntime(WithRuntimeConfig(cfg))
					defer rt.Close()
					module, e := rt.Compile(source)
					if e != nil {
						t.Fatal(e)
					}
					defer module.Close()
					in, e := rt.Instantiate(context.Background(), module, WithGC(mode.gc))
					if e != nil {
						t.Fatal(e)
					}
					defer in.Close()
					roots := in.c.GCNativeRootAdmission()
					if !roots.Required || !roots.Exact {
						t.Fatalf("root admission%+v", roots)
					}
					before := in.gc.Stats()
					got, e := in.Invoke("run")
					if e != nil || len(got) != 1 || got[0] != uint64(n*(n-1)/2) {
						t.Fatalf("sum%v err%v", got, e)
					}
					idx, e := in.Invoke("index")
					if e != nil || len(idx) != 1 || idx[0] != uint64(n) {
						t.Fatalf("index%v err%v", idx, e)
					}
					after := in.gc.Stats()
					if after.Allocations-before.Allocations != uint64(n+1) {
						t.Fatalf("logical delta%d want%d", after.Allocations-before.Allocations, n+1)
					}
					if mode.name == "native" {
						published := iteratorPublishedType(t, in, some)
						if enabled && published > 1 {
							t.Fatalf("physicalSome%d want<=1", published)
						}
						if !enabled && published != int(n) {
							t.Fatalf("control physicalSome%d want%d", published, n)
						}
					}
					if mode.name == "collect" && after.MinorCollections <= before.MinorCollections {
						t.Fatal("forced collection did not occur")
					}
				})
			}
		}
	}
}

func TestGCOptionViewSolePayloadRoots(t *testing.T) {
	for _, mode := range []string{"i31", "null", "graph"} {
		for _, enabled := range []bool{false, true} {
			for _, stress := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%t/%t", mode, enabled, stress), func(t *testing.T) {
					gcOptionTestSwitch(t, map[bool]string{false: "0", true: "1"}[enabled])
					source, _ := iteratorCarrierPayloadFixture(16, 130, 3, mode)
					decoded, e := wasm.DecodeModule(source)
					if e != nil {
						t.Fatal(e)
					}
					view, e := buildGCOptionBodyView(decoded)
					if e != nil || view == nil || view.Families != 1 {
						t.Fatalf("root test not admitted: view%v err%v", view, e)
					}
					rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1)))
					defer rt.Close()
					module, e := rt.Compile(source)
					if e != nil {
						t.Fatal(e)
					}
					defer module.Close()
					gc := GCConfig{StressNurseryBytes: 256, ThroughputHeapBytes: 16 << 20, VerifyAfterCollect: true}
					if stress {
						gc.CollectEveryAlloc = true
						gc.ForceMajorEveryMinor = true
					}
					in, e := rt.Instantiate(context.Background(), module, WithGC(gc))
					if e != nil {
						t.Fatal(e)
					}
					defer in.Close()
					before := in.gc.Stats()
					got, e := in.Invoke("run")
					want := uint64(120)
					if mode == "null" {
						want = 16
					}
					if mode == "graph" {
						want = 16*42 + 120
					}
					if e != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("payload result%v want%d error%v", got, want, e)
					}

					sum := got[0]
					idx, check := in.Invoke("index")
					if check != nil || len(idx) != 1 || idx[0] != 16 {
						t.Fatalf("source array not drained or index wrong: %v %v", idx, check)
					}
					after := in.gc.Stats()
					if after.Allocations-before.Allocations != 33 {
						t.Fatalf("logical%d want33", after.Allocations-before.Allocations)
					}
					if after.MinorCollections <= before.MinorCollections {
						t.Fatal("no collection after payload extraction")
					}
					// Every source array slot was cleared, so neither the array nor a global
					// can hide a missing native frame root for a returned payload graph.
					t.Logf("sum%d minor%d full%d logical33", sum, after.MinorCollections-before.MinorCollections, after.FullCollections-before.FullCollections)
				})
			}
		}
	}
}

func optionValidateTestModule(t *testing.T, m *wasm.Module) {
	t.Helper()
	var a wasm.ValidatedModuleAnalysis
	if e := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{CompactImports: true, MultiMemory: true, ExtendedConstGlobals: true, GCConstExpr: true}, 1, wasm.ValidationLimits{}, &a); e != nil {
		t.Fatalf("test input invalid: %v", e)
	}
}

func TestGCOptionViewRejectsUnsafeUses(t *testing.T) {
	for _, kind := range []string{"alias", "identity", "escape-returned-carrier", "extra-carrier-store", "oversize-body", "oversize-locals", "wrapper-shape", "tail-call"} {
		t.Run(kind, func(t *testing.T) {
			source, _ := iteratorCarrierFixture(16, 0, 0)
			m, e := wasm.DecodeModule(source)
			if e != nil {
				t.Fatal(e)
			}
			c := &m.Code[5]
			ins, _, ok := gcOptionDecode(m, c.BodyBytes, 4096)
			if !ok {
				t.Fatal("decode")
			}
			store := -1
			for i, x := range ins {
				if x.op == 0x21 && x.imm.Index == 1 {
					store = i
					break
				}
			}
			if store < 0 {
				t.Fatal("carrier store missing")
			}
			insert := func(pc int, b []byte) {
				out := append([]byte(nil), c.BodyBytes[:pc]...)
				out = append(out, b...)
				out = append(out, c.BodyBytes[pc:]...)
				c.BodyBytes = out
				c.Body = wasm.Expr{}
			}
			switch kind {
			case "alias":
				insert(ins[store].end, []byte{0x20, 1, 0x21, 2})
			case "identity":
				insert(ins[store].end, []byte{0x20, 1, 0x20, 1, 0xd3, 0x1a})
			case "escape-returned-carrier":
				insert(ins[store].end, iteratorFixtureSeq([]byte{0x23, 3}, iteratorFixtureCall(4), []byte{0x21, 1}))
			case "extra-carrier-store":
				insert(ins[store].end, []byte{0xd0, 0x6d, 0x21, 1})
			case "oversize-body":
				insert(0, bytes.Repeat([]byte{1}, 16385))
			case "oversize-locals":
				c.Locals.Runs = append(c.Locals.Runs, wasm.LocalRun{Count: 257, Type: wasm.I32})
			case "wrapper-shape":
				m.Code[4].BodyBytes = append([]byte{1}, m.Code[4].BodyBytes...)
				m.Code[4].Body = wasm.Expr{}
			case "tail-call":
				pc := ins[len(ins)-2].pc
				c.BodyBytes = append(append([]byte(nil), c.BodyBytes[:pc]...), 0x12, 6, 0xb)
				c.Body = wasm.Expr{}
			}
			optionValidateTestModule(t, m)
			before := sha256.Sum256(c.BodyBytes)
			view, e := buildGCOptionBodyView(m)
			if e != nil {
				t.Fatal(e)
			}
			if view != nil {
				t.Fatalf("unsafe or unsupported %s admitted: %d families", kind, view.Families)
			}
			if sha256.Sum256(c.BodyBytes) != before {
				t.Fatal("rejected body changed")
			}
		})
	}
}

func TestGCOptionViewTwoFamilies(t *testing.T) {
	source, some := iteratorCarrierFixture(16, 0, 0)
	m, e := wasm.DecodeModule(source)
	if e != nil {
		t.Fatal(e)
	}
	c := &m.Code[5]
	ins, _, ok := gcOptionDecode(m, c.BodyBytes, 4096)
	if !ok {
		t.Fatal("decode")
	}
	last := ins[len(ins)-2].pc
	var second []byte
	for _, x := range ins[2 : len(ins)-2] {
		if (x.op == 0x20 || x.op == 0x21) && x.imm.Index == 1 {
			second = append(second, x.op, 4)
		} else {
			second = append(second, x.raw...)
		}
	}
	reset := iteratorFixtureSeq([]byte{0x23, 3, 0x41, 0}, iteratorFixtureFB(5, 3, 1))
	body := append([]byte(nil), c.BodyBytes[:last]...)
	body = append(body, reset...)
	body = append(body, second...)
	body = append(body, c.BodyBytes[last:]...)
	c.BodyBytes = body
	c.Body = wasm.Expr{}
	c.Locals.Runs = append(c.Locals.Runs, wasm.LocalRun{Count: 1, Type: wasm.EqRef})
	optionValidateTestModule(t, m)
	view, e := buildGCOptionBodyView(m)
	if e != nil || view == nil || view.Families != 2 || len(view.Constructors) != 8 {
		t.Fatalf("two families%v err%v", view, e)
	}
	source = optionViewBinary(t, source, m)
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			gcOptionTestSwitch(t, map[bool]string{false: "0", true: "1"}[enabled])
			rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1)))
			defer rt.Close()
			module, e := rt.Compile(source)
			if e != nil {
				t.Fatal(e)
			}
			defer module.Close()
			in, e := rt.Instantiate(context.Background(), module, WithGC(GCConfig{NurseryBytes: 1 << 20, ThroughputHeapBytes: 16 << 20}))
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			before := in.gc.Stats()
			got, e := in.Invoke("run")
			if e != nil || len(got) != 1 || got[0] != 240 {
				t.Fatalf("sum%v want240 err%v", got, e)
			}
			if n := in.gc.Stats().Allocations - before.Allocations; n != 34 {
				t.Fatalf("logical%d want34", n)
			}
			n := iteratorPublishedType(t, in, some)
			if enabled && n > 1 || !enabled && n != 32 {
				t.Fatalf("physicalSome%d enabled%t", n, enabled)
			}
		})
	}
}

func TestGCOptionViewInvalidInitializer(t *testing.T) {
	for _, kind := range []string{"out-of-range", "free"} {
		var oracle string
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", kind, enabled), func(t *testing.T) {
				gcOptionTestSwitch(t, map[bool]string{false: "0", true: "1"}[enabled])
				source, _ := iteratorCarrierFixture(1, 0, 0)
				m, e := wasm.DecodeModule(source)
				if e != nil {
					t.Fatal(e)
				}
				m.Code[5].BodyBytes = bytes.Replace(m.Code[5].BodyBytes, []byte{0xfb, 22, 0x6c, 0xfb, 30}, []byte{0x1a, 0x41, 1}, 1)
				m.Code[5].Body = wasm.Expr{}
				source = optionViewBinary(t, source, m)
				rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1)))
				defer rt.Close()
				module, e := rt.Compile(source)
				if e != nil {
					t.Fatal(e)
				}
				defer module.Close()
				in, e := rt.Instantiate(context.Background(), module, WithGC(GCConfig{NurseryBytes: 1 << 20, ThroughputHeapBytes: 16 << 20}))
				if e != nil {
					t.Fatal(e)
				}
				defer in.Close()
				// The batch is live before corruption. Array element loads transfer the raw
				// word to the constructor; public arguments and casts do not inspect it.
				in.prepareNativeStructHandles(5)
				v := in.gc.NativeView()
				array := uint32(readGlobalObject(in.globalCells[1], in.c.Globals[1].Type))
				h := unsafe.Slice((*byte)(offHeapPtr(v.Handles+uintptr(array>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
				space := h[gc.NativeHandleSpaceOffset]
				offset := binary.LittleEndian.Uint32(h[gc.NativeHandleOffsetOffset:])
				if space == gc.NativeSpaceFree {
					t.Fatal("array is free")
				}
				payload := unsafe.Slice((*byte)(offHeapPtr(v.Spaces[space].Base+uintptr(offset)+uintptr(gc.PayloadOffset))), 12)
				bad := v.HandleCount << 1
				if kind == "free" {
					found := false
					entries := unsafe.Slice((*byte)(offHeapPtr(v.Handles)), int(v.HandleCount)*gc.NativeHandleStride)
					for id := uint32(1); id < v.HandleCount; id++ {
						if entries[int(id)*gc.NativeHandleStride+gc.NativeHandleSpaceOffset] == gc.NativeSpaceFree {
							bad = id << 1
							found = true
							break
						}
					}
					if !found {
						t.Fatal("reserved free handle missing")
					}
				}
				binary.LittleEndian.PutUint32(payload[0:4], bad)
				before := in.gc.Stats()
				_, e = in.Invoke("run")
				if e == nil {
					t.Fatal("invalid constructor reference accepted")
				}
				text := e.Error()
				if !strings.Contains(text, "gc struct") && !strings.Contains(text, "gc reference") {
					t.Fatalf("failure did not identify constructor/reference: %v", e)
				}
				if !enabled {
					oracle = text
				} else if text != oracle {
					t.Fatalf("initializer failure differs: off %q on %q", oracle, text)
				}
				if n := in.gc.Stats().Allocations - before.Allocations; n != 0 {
					t.Fatalf("invalid ctor charged%d objects", n)
				}
				t.Logf("rejected at initialized helper: %v", e)
			})
		}
	}
}
