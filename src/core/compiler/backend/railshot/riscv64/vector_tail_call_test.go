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

func TestVectorLocalTailExactSignatureBounds(t *testing.T) {
	carriers := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128}
	for _, a := range carriers {
		for _, b := range carriers {
			caller := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{a}}
			callee := &wasm.CompType{Kind: wasm.CompFunc, Params: carriers, Results: []wasm.ValType{b}}
			err := admitSWARLocalTailSignature(caller, callee)
			if (err == nil) != wasm.EqualValType(a, b) {
				t.Fatal("exact result type", a, b, err)
			}
			if wasm.EqualValType(b, wasm.V128) && admitLocalTailSignature(caller, callee) == nil {
				t.Fatal("scalar tail admitted vector")
			}
		}
	}
	for _, n := range []int{maxSWARLocals, maxSWARLocals + 1} {
		list := make([]wasm.ValType, n)
		for i := range list {
			list[i] = wasm.V128
		}
		for _, wideParams := range []bool{false, true} {
			sig := &wasm.CompType{Kind: wasm.CompFunc}
			if wideParams {
				sig.Params = list
			} else {
				sig.Results = list
			}
			err := admitSWARLocalTailSignature(sig, sig)
			if (err == nil) != (n == maxSWARLocals) {
				t.Fatal("logical bound", n, wideParams, err)
			}
			if n == maxSWARLocals && swarSignatureSlots(list) != 2*maxSWARLocals {
				t.Fatal("physical slot count")
			}
		}
	}
	for _, bad := range []*wasm.CompType{nil, {Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}}, {Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.FuncRef}}} {
		if admitSWARLocalTailSignature(bad, bad) == nil {
			t.Fatal("invalid signature admitted", bad)
		}
	}
	// Equal physical result width alone does not establish result arity/types.
	a := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.V128}}
	b := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I64, wasm.I64}}
	if admitSWARLocalTailSignature(a, b) == nil {
		t.Fatal("two scalar results treated as one vector")
	}
}

func TestVectorLocalTailPrivateAndUnsupportedGates(t *testing.T) {
	v := []wasm.ValType{wasm.V128}
	source := localTailSource(true, localTailFixture{params: v, results: v, body: []byte{0x20, 0, 0x12, 1, 0x0b}}, localTailFixture{params: v, results: v, body: []byte{0x20, 0, 0x0b}})
	for _, opts := range []CompileOptions{{Interruptible: true, DeferCodeMapping: true}, {localTailCalls: true, DeferCodeMapping: true}, {localTailCalls: true, Interruptible: true, DeferCodeMapping: true, Profile: true}, {localTailCalls: true, Interruptible: true, DeferCodeMapping: true, SourceMaps: true}, {localTailCalls: true, Interruptible: true, DeferCodeMapping: true, UnwindMaps: true}} {
		pressure := 0
		opts.MemoryPressure = func() { pressure++ }
		image, err := CompileModuleWith(localTailDecode(t, source), opts)
		if !errors.Is(err, ErrUnsupported) || image != nil || pressure != 0 {
			t.Fatal("vector tail policy staged", err, pressure)
		}
	}
	if c, err := CompileWithOptions(source, CompileOptions{localTailCalls: true}); !errors.Is(err, ErrUnsupported) || c != nil {
		t.Fatal("isolated gate", err)
	}
	// This direct same-walker witness cannot pass merely because a decoder or
	// normal validator rejects a missing table/reference dependency first.
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: v, Results: v}
	for _, body := range [][]byte{{0x00, 0x12, 2, 0x0b}, {0x00, 0x13, 0, 0, 0x0b}, {0x00, 0x15, 0, 0x0b}} {
		fn := &wasm.Func{BodyBytes: body}
		if _, err := walkSWARPairsWithTailMode(fn, ft, memoryProfile{}, nil, []*wasm.CompType{ft}, nil, 0, nil, nil, 0, 0, false, false, true, nil); !errors.Is(err, ErrUnsupported) {
			t.Fatal("dead unsupported vector tail walker", body, err)
		}
	}
}

func TestVectorLocalTailPhysicalTransferBudget(t *testing.T) {
	v := make([]wasm.ValType, 128)
	for i := range v {
		v[i] = wasm.V128
	}
	leaf := []byte{}
	for i := range v {
		leaf = append(leaf, 0x20)
		leaf = append(leaf, wasmtest.ULEB(uint32(i))...)
	}
	leaf = append(leaf, 0x0b)
	// 31*(1opcode+256args+256results)+unreachable+end <=16384;
	// 32 iterations exceed the limit despite only128 logical values per side.
	for _, n := range []int{31, 32} {
		dead := []byte{0x00}
		for range n {
			dead = append(dead, 0x12, 1)
		}
		dead = append(dead, 0x0b)
		m := localTailDecode(t, localTailSource(true, localTailFixture{results: v, body: dead}, localTailFixture{params: v, results: v, body: leaf}))
		pressure := 0
		opts := localTailOptions()
		opts.MemoryPressure = func() { pressure++ }
		image, err := CompileModuleWith(m, opts)
		if n == 31 {
			if err != nil || image == nil || image.CodeImage != nil || pressure != 1 {
				t.Fatal("physical positive boundary", err, pressure)
			}
		} else if !errors.Is(err, ErrUnsupported) || image != nil || pressure != 0 {
			t.Fatal("physical overflow staged", err, pressure)
		}
	}
}

func TestVectorLocalTailSourceArityBoundBeforeStaging(t *testing.T) {
	for _, n := range []int{maxSWARLocals, maxSWARLocals + 1} {
		v := make([]wasm.ValType, n)
		body := []byte{}
		for i := range v {
			v[i] = wasm.V128
			body = append(body, 0x20)
			body = append(body, wasmtest.ULEB(uint32(i))...)
		}
		root := append(append([]byte{}, body...), 0x12, 1, 0x0b)
		leaf := append(append([]byte{}, body...), 0x0b)
		m := localTailDecode(t, localTailSource(true, localTailFixture{params: v, results: v, body: root}, localTailFixture{params: v, results: v, body: leaf}))
		pressure := 0
		opts := localTailOptions()
		opts.MemoryPressure = func() { pressure++ }
		image, err := CompileModuleWith(m, opts)
		if n == maxSWARLocals {
			if err != nil || image == nil || pressure != 1 {
				t.Fatal("maximum source shape", err, pressure)
			}
		} else if !errors.Is(err, ErrUnsupported) || image != nil || pressure != 0 {
			t.Fatal("overbound source shape staged", err, pressure)
		}
	}
}

func TestVectorLocalTailLinksAndOrdinaryCodeIdentity(t *testing.T) {
	v := []wasm.ValType{wasm.V128}
	funcs := []localTailFixture{{params: v, results: v, body: []byte{0x20, 0, 0x12, 1, 0x0b}}, {params: v, results: v, body: []byte{0x20, 0, 0x12, 0, 0x0b}}, {params: v, results: v, body: []byte{0x20, 0, 0x12, 2, 0x0b}}}
	image, err := CompileModuleWith(localTailDecode(t, localTailSource(true, funcs...)), localTailOptions())
	if err != nil {
		t.Fatal(err)
	}
	for i, target := range []int{image.Entry[1], image.Entry[0], image.Entry[2]} {
		end := len(image.Code)
		if i+1 < len(image.Entry) {
			end = image.Entry[i+1]
		}
		matches := 0
		for off := image.Entry[i]; off+8 <= end; off += 4 {
			hi := binary.LittleEndian.Uint32(image.Code[off:])
			lo := binary.LittleEndian.Uint32(image.Code[off+4:])
			if hi&0x7f != 0x17 || (hi>>7)&31 != 31 || lo&0x7f != 0x67 || (lo>>7)&31 != 0 || (lo>>15)&31 != 31 {
				continue
			}
			dest := int64(off) + int64(int32(hi&0xfffff000)) + int64(int32(lo)>>20)
			if dest == int64(target) {
				matches++
				if off < 4 || binary.LittleEndian.Uint32(image.Code[off-4:]) != 0x01e10133 {
					t.Fatal("vector tail omitted frame release")
				}
			}
		}
		if matches != 1 {
			t.Fatal("vector nonlink target", i, matches, target)
		}
	}
	ordinary := localTailSource(true, localTailFixture{params: v, results: v, body: []byte{0x20, 0, 0x10, 1, 0x0b}}, localTailFixture{params: v, results: v, body: []byte{0x20, 0, 0x0b}})
	a, err := CompileModuleWith(localTailDecode(t, ordinary), CompileOptions{DeferCodeMapping: true, Interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := CompileModuleWith(localTailDecode(t, ordinary), localTailOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Code, b.Code) {
		t.Fatal("ordinary vector code changed")
	}
}

func TestVectorLocalTailImportedTargetStillRejectsBeforeStaging(t *testing.T) {
	v := []wasm.ValType{wasm.V128}
	im := append(wasmtest.Name("m"), wasmtest.Name("f")...)
	im = append(im, 0, 0)
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(v, v))), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x12, 0, 0x0b}))))
	m := localTailDecode(t, source)
	pressure := 0
	opts := localTailOptions()
	opts.ImportBindings = []ImportBinding{{Dynamic: true, ImportIndex: 0}}
	opts.MemoryPressure = func() { pressure++ }
	image, err := CompileModuleWith(m, opts)
	if !errors.Is(err, ErrUnsupported) || image != nil || pressure != 0 {
		t.Fatal("imported vector tail staged", err, pressure)
	}
}
