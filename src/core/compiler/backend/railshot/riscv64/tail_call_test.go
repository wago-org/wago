//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type localTailFixture struct {
	params, results []wasm.ValType
	body            []byte
	locals          int
}

func localTailSource(vectorProfile bool, funcs ...localTailFixture) []byte {
	var types, codes [][]byte
	indices := wasmtest.ULEB(uint32(len(funcs)))
	for i, f := range funcs {
		types = append(types, wasmtest.FuncType(f.params, f.results))
		indices = append(indices, wasmtest.ULEB(uint32(i))...)
		decls := []byte{0}
		if f.locals != 0 {
			decls = append([]byte{1}, wasmtest.ULEB(uint32(f.locals))...)
			decls = append(decls, 0x7e)
		}
		body := append(decls, f.body...)
		codes = append(codes, append(wasmtest.ULEB(uint32(len(body))), body...))
	}
	if vectorProfile {
		// Force the genuine SWAR route without enabling vector tail signatures.
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, indices),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func localTailDecode(t *testing.T, source []byte) *wasm.Module {
	t.Helper()
	d, err := wasm.DecodeModuleByteBackedWithLimits(source, wasm.ValidationFeatures{}, wasm.DecodeLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(d.Module, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: wasm.DefaultMaxFunctionLocals}); err != nil {
		t.Fatal(err)
	}
	return d.Module
}

func localTailOptions() CompileOptions {
	return CompileOptions{localTailCalls: true, Interruptible: true, DeferCodeMapping: true}
}

func TestLocalTailCheckedFrameLayout(t *testing.T) {
	for _, tc := range []struct{ header, locals, slots, want int }{
		{0, 0, 0, 16}, {48, 0, 0, 48}, {48, 256, 512, 6192},
		{16, 0, (MaxFrameBytes - 16) / 8, MaxFrameBytes},
	} {
		got, err := checkedFrameBytes(tc.header, tc.locals, tc.slots)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	maxInt := int(^uint(0) >> 1)
	for _, tc := range [][3]int{{-1, 0, 0}, {17, 0, 0}, {8, 0, 0}, {48, -1, 0}, {48, 0, -1}, {48, maxInt, 0}, {48, 0, maxInt}, {MaxFrameBytes, 1, 0}} {
		if _, err := checkedFrameBytes(tc[0], tc[1], tc[2]); err == nil {
			t.Fatal("unsafe frame admitted", tc)
		}
	}
	for _, frame := range []int{0, 47, 63, MaxFrameBytes + 16} {
		f := integerEmitter{target: target{header: 48, mode: emissionMode{production: true, interruptible: true, localTailCalls: true}}, frameBytes: frame}
		f.localTailJump(0)
		if f.err == nil || len(f.a.B) != 0 || len(f.calls) != 0 {
			t.Fatal("invalid tail emitted", frame)
		}
	}
}

func TestLocalTailSignatureUsesExactTypes(t *testing.T) {
	for _, left := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64} {
		for _, right := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128, wasm.FuncRef} {
			a := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{left}}
			b := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{right}}
			err := admitLocalTailSignature(a, b)
			if (err == nil) != wasm.EqualValType(left, right) {
				t.Fatal(left, right, err)
			}
		}
	}
}

func TestLocalTailAdmissionAndPrivateClosure(t *testing.T) {
	for _, vector := range []bool{false, true} {
		source := localTailSource(vector, localTailFixture{body: []byte{0x12, 1, 0x0b}}, localTailFixture{body: []byte{0x0b}})
		for _, opts := range []CompileOptions{
			{DeferCodeMapping: true, Interruptible: true},
			{DeferCodeMapping: true, localTailCalls: true},
			{DeferCodeMapping: true, Interruptible: true, localTailCalls: true, Profile: true},
			{DeferCodeMapping: true, Interruptible: true, localTailCalls: true, SourceMaps: true},
			{DeferCodeMapping: true, Interruptible: true, localTailCalls: true, UnwindMaps: true},
		} {
			pressure := 0
			opts.MemoryPressure = func() { pressure++ }
			image, err := CompileModuleWith(localTailDecode(t, source), opts)
			if !errors.Is(err, ErrUnsupported) || image != nil || pressure != 0 {
				t.Fatal("tail policy staged code", vector, err, pressure)
			}
		}
		image, err := CompileModuleWith(localTailDecode(t, source), localTailOptions())
		if err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != 2 {
			t.Fatal("internal local tail", vector, err)
		}
		if c, err := CompileWithOptions(source, CompileOptions{localTailCalls: true}); !errors.Is(err, ErrUnsupported) || c != nil {
			t.Fatal("isolated tail gate", vector, err)
		}
	}
}

func TestLocalTailForwardBackwardSelfJumpsDecodeIndependently(t *testing.T) {
	for _, vector := range []bool{false, true} {
		funcs := []localTailFixture{
			{body: []byte{0x12, 1, 0x0b}},
			{body: []byte{0x12, 0, 0x0b}},
			{body: []byte{0x12, 2, 0x0b}},
		}
		image, err := CompileModuleWith(localTailDecode(t, localTailSource(vector, funcs...)), localTailOptions())
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range []int{image.Entry[1], image.Entry[0], image.Entry[2]} {
			start, end := image.Entry[i], len(image.Code)
			if i+1 < len(image.Entry) {
				end = image.Entry[i+1]
			}
			matches := 0
			for off := start; off+8 <= end; off += 4 {
				hi := binary.LittleEndian.Uint32(image.Code[off:])
				lo := binary.LittleEndian.Uint32(image.Code[off+4:])
				if hi&0x7f != 0x17 || (hi>>7)&31 != 31 || lo&0x7f != 0x67 || (lo>>7)&31 != 0 || (lo>>15)&31 != 31 {
					continue
				}
				target := int64(off) + int64(int32(hi&0xfffff000)) + int64(int32(lo)>>20)
				if target == int64(want) {
					matches++
					// Independently identify ADD SP,SP,X30 immediately before jump.
					if off < start+4 || binary.LittleEndian.Uint32(image.Code[off-4:]) != 0x01e10133 {
						t.Fatal("missing exact frame release", i, off)
					}
				}
			}
			if matches != 1 {
				t.Fatal("missing/duplicate nonlink tail", vector, i, matches, want)
			}
		}
	}
}

func TestLocalTailHighArityAndDeadBudget(t *testing.T) {
	for _, vector := range []bool{false, true} {
		arity := 256
		if vector {
			arity = 128
		}
		params := make([]wasm.ValType, arity)
		body := []byte{}
		for i := range params {
			params[i] = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}[i%4]
			body = append(body, 0x20)
			body = append(body, wasmtest.ULEB(uint32(i))...)
		}
		body = append(body, 0x12, 1, 0x0b)
		leaf := []byte{}
		for i := range params {
			leaf = append(leaf, 0x20)
			leaf = append(leaf, wasmtest.ULEB(uint32(i))...)
		}
		leaf = append(leaf, 0x0b)
		m := localTailDecode(t, localTailSource(vector, localTailFixture{params: params, results: params, body: body}, localTailFixture{params: params, results: params, body: leaf}))
		image, err := CompileModuleWith(m, localTailOptions())
		if err != nil || len(image.Entry) != 2 {
			t.Fatal("full bounded arity", vector, err)
		}
		// Every dead tail still consumes the call-count and signature budget.
		for _, count := range []int{1024, 1025} {
			dead := []byte{0x00}
			for range count {
				dead = append(dead, 0x12, 1)
			}
			dead = append(dead, 0x0b)
			m := localTailDecode(t, localTailSource(vector, localTailFixture{body: dead}, localTailFixture{body: []byte{0x0b}}))
			pressure := 0
			opts := localTailOptions()
			opts.MemoryPressure = func() { pressure++ }
			image, err := CompileModuleWith(m, opts)
			if count == 1024 {
				if err != nil {
					t.Fatal("dead count boundary", vector, err)
				}
			} else if !errors.Is(err, ErrUnsupported) || image != nil || pressure != 0 {
				t.Fatal("dead tail budget staged", vector, err, pressure)
			}
		}
	}
}

func TestLocalTailUnsupportedFamiliesRemainClosedEvenDead(t *testing.T) {
	for _, vector := range []bool{false, true} {
		// Both are valid encodings; the unsupported type/table instruction shapes
		// are tested directly on the same bounded walker, not decoder rejection.
		for _, tail := range [][]byte{{0x13, 0, 0}, {0x15, 0}} {
			fn := &wasm.Func{BodyBytes: append(append([]byte{0x00}, tail...), 0x0b)}
			ft := &wasm.CompType{Kind: wasm.CompFunc}
			state := moduleState{production: true, localTailCalls: true}
			if vector {
				if _, err := walkSWARPairsWithTailMode(fn, ft, memoryProfile{}, nil, nil, nil, 0, nil, nil, 0, 0, false, false, true, nil); !errors.Is(err, ErrUnsupported) {
					t.Fatal("dead tail family", tail, err)
				}
			} else if _, err := walkInteger(fn, ft, &state, nil); !errors.Is(err, ErrUnsupported) {
				t.Fatal("dead tail family", tail, err)
			}
		}
		// An imported exact-void target is normally validated but is not C2a.
		imports := append(wasmtest.Name("m"), wasmtest.Name("f")...)
		imports = append(imports, 0, 0)
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(2, wasmtest.Vec(imports)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x12, 0, 0x0b}))))
		m := localTailDecode(t, source)
		if vector {
			m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}}}})
		}
		opts := localTailOptions()
		opts.ImportBindings = []ImportBinding{{Dynamic: true, ImportIndex: 0}}
		image, err := CompileModuleWith(m, opts)
		if !errors.Is(err, ErrUnsupported) || image != nil {
			t.Fatal("import tail was admitted", vector, err)
		}
	}
}

func TestLocalTailOrdinaryModuleCodeIdentity(t *testing.T) {
	for _, vector := range []bool{false, true} {
		source := localTailSource(vector, localTailFixture{results: []wasm.ValType{wasm.I32}, body: []byte{0x10, 1, 0x0b}}, localTailFixture{results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 9, 0x0b}})
		a, err := CompileModuleWith(localTailDecode(t, source), CompileOptions{Interruptible: true, DeferCodeMapping: true})
		if err != nil {
			t.Fatal(err)
		}
		b, err := CompileModuleWith(localTailDecode(t, source), localTailOptions())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a.Code, b.Code) {
			t.Fatal("ordinary module changed", vector)
		}
	}
}

func TestLocalTailMalformedHiddenBodiesRejectBeforeStaging(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, body := range [][]byte{
			{0x12, 0x80},          // truncated index
			{0x12, 2, 0x0b},       // unknown function
			{0x00, 0x12, 2, 0x0b}, // unknown dead target
			{0x00, 0x12, 0x80, 0x80, 0x80, 0x80, 0x80, 0, 0x0b}, // oversized u32
		} {
			source := localTailSource(vector, localTailFixture{body: []byte{0x0b}}, localTailFixture{body: body})
			m, err := wasm.DecodeModule(source)
			if err != nil {
				continue
			} // A real decoder denial is also before staging.
			pressure := 0
			opts := localTailOptions()
			opts.MemoryPressure = func() { pressure++ }
			image, err := CompileModuleWith(m, opts)
			if err == nil || image != nil || pressure != 0 {
				t.Fatal("hidden malformed tail staged", vector, body, err, pressure)
			}
		}
		// Different scalar results have the same physical width but still fail
		// normal type validation before native emission or image staging.
		source := localTailSource(vector, localTailFixture{results: []wasm.ValType{wasm.I32}, body: []byte{0x12, 1, 0x0b}}, localTailFixture{results: []wasm.ValType{wasm.F32}, body: []byte{0x43, 0, 0, 0, 0, 0x0b}})
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		pressure := 0
		opts := localTailOptions()
		opts.MemoryPressure = func() { pressure++ }
		image, err := CompileModuleWith(m, opts)
		if err == nil || image != nil || pressure != 0 {
			t.Fatal("same-width wrong result staged", vector, err, pressure)
		}
	}
}
