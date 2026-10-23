//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDirectForwarderBounds(t *testing.T) {
	for _, hops := range []int{0, 1, 4, 5} {
		t.Run(fmt.Sprint(hops), func(t *testing.T) {
			defs := make([]funcDef, hops+1)
			for i := range defs {
				defs[i] = funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x10, byte(i + 1), 0x0b}}
			}
			defs[hops].body = []byte{0, 0x20, 0, 0x0b}
			m := modFuncs(t, defs...)
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			got, ok := directForwarderTarget(m, 0, 0)
			want := hops > 0 && hops <= maxDirectForwarderHops
			if ok != want || want && got != uint32(hops) {
				t.Fatalf("target=%d,%t for %d hops", got, ok, hops)
			}
		})
	}
}

func TestDirectForwarderRejectsNonIdentity(t *testing.T) {
	for _, body := range [][]byte{
		{1, 1, 0x7f, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}, // declared local
		{0, 0x20, 1, 0x20, 0, 0x10, 1, 0x0b},          // reordered inputs
		{0, 0x20, 0, 0x20, 0, 0x10, 1, 0x0b},          // repeated input
		{0, 0x01, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b},    // extra instruction
		{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x01, 0x0b},    // work after call
		{0, 0x20, 0, 0x20, 1, 0x12, 1, 0x0b},          // return_call is not call
	} {
		ft := []wasm.ValType{wasm.I32, wasm.I32}
		m := modFuncs(t, funcDef{params: ft, results: ft[:1], body: body}, funcDef{params: ft, results: ft[:1], body: []byte{0, 0x20, 0, 0x0b}})
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		if target, ok := directForwarderTarget(m, 0, 0); ok {
			t.Fatalf("accepted nonidentity %x -> %d", body, target)
		}
	}
	for _, cycle := range []int{1, 2, 5} {
		defs := make([]funcDef, cycle)
		for i := range defs {
			defs[i].body = []byte{0, 0x10, byte((i + 1) % cycle), 0x0b}
		}
		m := modFuncs(t, defs...)
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		if target, ok := directForwarderTarget(m, 0, 0); ok {
			t.Fatalf("accepted %d-cycle ->%d", cycle, target)
		}
	}
}

func TestDirectForwarderExactReferenceTypes(t *testing.T) {
	any := wasm.RefVal(wasm.AbsRef(wasm.HeapAny))
	eq := wasm.RefVal(wasm.AbsRef(wasm.HeapEq))
	for _, paramChange := range []bool{false, true} {
		wrapperParam, targetResult := any, eq
		if paramChange {
			wrapperParam, targetResult = eq, any
		}
		m := modFuncs(t,
			funcDef{params: []wasm.ValType{wrapperParam}, results: []wasm.ValType{any}, body: []byte{0, 0x20, 0, 0x10, 1, 0x0b}},
			funcDef{params: []wasm.ValType{any}, results: []wasm.ValType{targetResult}, body: []byte{0, 0xd0, 0x6d, 0x0b}},
		)
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		if target, ok := directForwarderTarget(m, 0, 0); ok {
			t.Fatalf("accepted same-machine-class reference mismatch ->%d", target)
		}
	}
	if equalForwarderTypes([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.F64}) {
		t.Fatal("equal-width integer/float types must differ")
	}
	nonnull := wasm.RefVal(any.Ref().WithNullable(false))
	if equalForwarderTypes([]wasm.ValType{any}, []wasm.ValType{nonnull}) {
		t.Fatal("reference nullability must match exactly")
	}
}

func TestDirectForwarderBodyLimit(t *testing.T) {
	const params = 80
	types := make([]wasm.ValType, params)
	wrapper, target := []byte{0}, []byte{0}
	for i := range types {
		types[i] = wasm.I32
		wrapper = append(wrapper, 0x20, byte(i))
		target = append(target, 0x20, byte(i), 0x1a)
	}
	wrapper = append(wrapper, 0x10, 1, 0x0b)
	target = append(target, 0x0b)
	m := modFuncs(t, funcDef{params: types, body: wrapper}, funcDef{params: types, body: target})
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if target, ok := directForwarderTarget(m, 0, 0); ok {
		t.Fatalf("accepted oversized wrapper -> %d", target)
	}
}

func TestDirectForwarderStopsAtImport(t *testing.T) {
	imp := append(wasmtest.Name("host"), wasmtest.Name("call")...)
	imp = append(imp, 0, 0)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}), wasmtest.Code([]byte{0x10, 1, 0x0b}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, first := range []uint32{0, 1, 2} {
		if target, ok := directForwarderTarget(m, 1, first); ok {
			t.Fatalf("import chain %d -> %d", first, target)
		}
	}
}

// A leaf's sole source call is inside the last wrapper, so compact layout can
// omit its standalone body. Calls to earlier wrappers must not acquire a real
// relocation to that omitted entry after inline planning has already finished.
func TestDirectForwarderCompactOmittedLeaf(t *testing.T) {
	before := inlineDeadBodyEnabled
	inlineDeadBodyEnabled = true
	t.Cleanup(func() { inlineDeadBodyEnabled = before })
	for _, leaf := range []struct {
		name string
		body []byte
		want uint32
	}{
		{"identity", []byte{0, 0x20, 0, 0x0b}, 5},
		{"add-constant", []byte{0, 0x20, 0, 0x41, 1, 0x6a, 0x0b}, 6},
	} {
		for _, hops := range []int{1, maxDirectForwarderHops} {
			for _, workers := range []int{1, 2} {
				t.Run(fmt.Sprintf("%s/hops%d/workers%d", leaf.name, hops, workers), func(t *testing.T) {
					defs := []funcDef{{results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x41, 5, 0x10, 1, 0x0b}}}
					for i := 1; i <= hops; i++ {
						defs = append(defs, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x10, byte(i + 1), 0x0b}})
					}
					defs = append(defs, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf.body})
					m := modFuncs(t, defs...)
					if err := wasm.ValidateModule(m); err != nil {
						t.Fatal(err)
					}
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Workers: workers, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"inline": true, "direct-forwarder": true}})
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					last := len(defs) - 1
					if cm.Entry[last] != cm.InternalEntry[0] || cm.InternalEntry[last] != cm.InternalEntry[0] {
						t.Fatalf("fixture must omit leaf body: entry=%v internal=%v", cm.Entry, cm.InternalEntry)
					}
					if diagnosticsEnabled {
						if stats.Funcs[last].Peephole["inline-dead-body"] != 1 || stats.Funcs[last].CodeBytes != 0 || stats.Funcs[hops].Calls["inline"] != 1 {
							t.Fatalf("leaf omission/inline proof changed: leaf=%+v wrapper=%+v", stats.Funcs[last], stats.Funcs[hops])
						}
						for i := 0; i < hops; i++ {
							if got := stats.Funcs[i].Peephole["direct-forwarder-retarget"]; got != 0 {
								t.Fatalf("caller %d retargeted %d calls to omitted leaf", i, got)
							}
						}
					}
					if got := uint32(runCompiledAmd64u(t, cm)); got != leaf.want {
						t.Fatalf("result=%d, want %d", got, leaf.want)
					}
				})
			}
		}
	}
}

func TestDirectForwarderRetainsNormalInlineTargetLowering(t *testing.T) {
	before := inlineDeadBodyEnabled
	t.Cleanup(func() { inlineDeadBodyEnabled = before })
	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprintf("compact%t", compact), func(t *testing.T) {
			// Ordinary mode never omits this leaf; compact rollback must also
			// retain the existing retarget when omission is explicitly disabled.
			inlineDeadBodyEnabled = !compact
			m := modFuncs(t,
				funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x41, 5, 0x10, 1, 0x0b}},
				funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x10, 2, 0x0b}},
				funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x41, 1, 0x6a, 0x0b}},
			)
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{CompactNative: compact, Workers: 1, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"inline": true, "direct-forwarder": true}})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if cm.InternalEntry[2] == cm.InternalEntry[0] {
				t.Fatal("leaf body was omitted")
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["direct-forwarder-retarget"] != 1 {
				t.Fatalf("retained leaf must still be a direct target: %+v", stats.Funcs[0])
			}
			if got := uint32(runCompiledAmd64u(t, cm)); got != 6 {
				t.Fatalf("result=%d, want 6", got)
			}
		})
	}
}
