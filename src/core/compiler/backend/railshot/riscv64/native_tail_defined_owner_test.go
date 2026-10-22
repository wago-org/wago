//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestNativeTailDefinedOwnerActualFrameAndFrozenSignature(t *testing.T) {
	sig := wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.F32}}
	f := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
	o := tailDynamicOwnerForTest(t, []tailRAFunction{f})
	f, lease := tailPublishedFunctionForTest(t, o.functions[0])
	owner, err := NewNativeTailDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, []int{0}, []wasm.CompType{sig})
	if err != nil || owner.owner.functions[0].info.FrameBytes != f.info.FrameBytes {
		t.Fatal("derived actual frame", err)
	}
	sig.Params[0] = wasm.F64
	if err := owner.Validate(); err != nil || !wasm.EqualValType(owner.owner.functions[0].signature.Params[0], wasm.I32) {
		t.Fatal("caller signature mutation escaped snapshot", err)
	}
	if n := testing.AllocsPerRun(4, func() {
		if err := owner.Validate(); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm defined-owner authentication allocation", n)
	}
	for _, data := range [][]byte{o.context[16:24], o.descriptors[8:24], o.descriptors[32:40], o.descriptors[40:80], o.directory} {
		for off := 0; off+8 <= len(data); off += 8 {
			before := binary.LittleEndian.Uint64(data[off:])
			binary.LittleEndian.PutUint64(data[off:], before^8)
			if err := owner.Validate(); !errors.Is(err, ErrUnsupported) {
				t.Fatal("changed canonical owner accepted", off, err)
			}
			binary.LittleEndian.PutUint64(data[off:], before)
		}
	}
	lease.err = errors.New("physical lease retired")
	if err := owner.Validate(); !errors.Is(err, lease.err) {
		t.Fatal("retired physical owner reused", err)
	}
}

func TestNativeTailDefinedOwnerBoundsBeforeFactoryPublication(t *testing.T) {
	sig := wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	f := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
	o := tailDynamicOwnerForTest(t, []tailRAFunction{f})
	f, _ = tailPublishedFunctionForTest(t, o.functions[0])
	for _, entry := range []int{-16, 4, 16, len(f.snapshot)} {
		if owner, err := NewNativeTailDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, []int{entry}, []wasm.CompType{sig}); owner != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid native entry accepted", entry, err)
		}
	}
	for _, end := range []int{0, 4, 16, len(f.snapshot) + 4} {
		if owner, err := NewNativeTailDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, []int{0, end}, []wasm.CompType{sig}); owner != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid next-function boundary accepted", end, err)
		}
	}
	for _, pair := range [][2]int{{0, 0}, {MaxFunctions + 1, 0}, {1, 513}, {MaxFunctions, MaxFunctions * 512}} {
		if err := NativeTailDefinedOwnerBudget(pair[0], pair[1]); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unbounded metadata accepted", pair, err)
		}
	}
	owner, err := NewNativeTailDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, []int{0}, []wasm.CompType{sig})
	if err != nil {
		t.Fatal(err)
	}
	s := tailDynamicSiteForTest(t, f, nil, f.info.FrameBytes-8)
	s.dynamic = nil
	before := bytes.Clone(o.context)
	for _, owners := range [][]*NativeTailDefinedOwner{nil, {nil}, {owner, owner}, make([]*NativeTailDefinedOwner, tailRAMaxRanges+1)} {
		if _, err := NativeTailDefinedSite(s, f.info.FrameBytes-8, owners); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid defined owner collection", err)
		}
	}
	if _, err := NativeTailDefinedSite(s, 48, []*NativeTailDefinedOwner{owner}); !errors.Is(err, ErrUnsupported) {
		t.Fatal("reference overlapped canonical argument home", err)
	}
	if !bytes.Equal(before, o.context) {
		t.Fatal("rejected owner/site published a capability")
	}
}

func TestNativeTailDefinedOwnerSignatureBoundsAndWarmValidation(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.V128} {
		count := 256
		if wasm.EqualValType(typ, wasm.V128) {
			count = 128
		}
		values := make([]wasm.ValType, count)
		for i := range values {
			values[i] = typ
		}
		sig := wasm.CompType{Kind: wasm.CompFunc, Params: values, Results: values}
		f := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
		o := tailDynamicOwnerForTest(t, []tailRAFunction{f})
		f, _ = tailPublishedFunctionForTest(t, o.functions[0])
		owner, err := NewNativeTailDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, []int{0}, []wasm.CompType{sig})
		if err != nil {
			t.Fatal("full physical signature", err)
		}
		if n := testing.AllocsPerRun(4, func() {
			if err := owner.Validate(); err != nil {
				panic(err)
			}
		}); n != 0 {
			t.Fatal("large frozen signature warm validation allocations", n)
		}
		for _, bad := range []wasm.CompType{
			{Kind: wasm.CompFunc, Params: append(append([]wasm.ValType(nil), values...), typ), Results: values},
			{Kind: wasm.CompFunc, Params: values, Results: append(append([]wasm.ValType(nil), values...), typ)},
			{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}},
			{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.FuncRef}},
		} {
			if rejected, err := NewNativeTailDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, []int{0}, []wasm.CompType{bad}); rejected != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("unsupported signature accepted", err)
			}
		}
		// Both signature and key belong to the opaque owner. A frozen factory
		// makes its own copy and still rejects changed canonical descriptor keys.
		frozen := (&tailRADynamicSite{owners: []tailRADynamicOwner{owner.owner}}).freeze()
		owner.owner.signatureKeys[0] ^= 8
		if err := owner.Validate(); !errors.Is(err, ErrUnsupported) {
			t.Fatal("changed expected signature key accepted", err)
		}
		if _, _, err := frozen.budget(); err != nil {
			t.Fatal(err)
		}
		if err := frozen.owners[0].validate(0); err != nil {
			t.Fatal("factory signature key aliases owner metadata", err)
		}
		owner.owner.signatureKeys = []uint64{}
		if err := owner.Validate(); !errors.Is(err, ErrUnsupported) {
			t.Fatal("missing frozen signature key accepted", err)
		}
	}
}

func TestNativeTailDefinedOwnerMultipleFunctionSpans(t *testing.T) {
	signatures := []wasm.CompType{
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.F32}},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64, wasm.F64}, Results: []wasm.ValType{wasm.V128}},
	}
	functions := []tailRAFunction{
		tailRAProofFunctionFixture(t, &signatures[0], abi.RV64InstanceContextBytes),
		tailRAProofFunctionFixture(t, &signatures[1], abi.RV64InstanceContextBytes),
	}
	second := (len(functions[0].snapshot) + 15) &^ 15
	code := make([]byte, second)
	copy(code, functions[0].snapshot)
	code = append(code, functions[1].snapshot...)
	buffer := tailRAProofBuffer(t, code)
	for i := range functions {
		functions[i].code, functions[i].snapshot = buffer, code
		functions[i].job, functions[i].context = functions[0].job, functions[0].context
		functions[i].info.mode.functionIndex = uint32(i)
	}
	functions[0].info.CodeBytes = second
	functions[1].info.Entry = second
	o := tailDynamicOwnerForTest(t, functions)
	f, _ := tailPublishedFunctionForTest(t, o.functions[0])
	owner, err := NewNativeTailDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, []int{0, second}, signatures)
	if err != nil {
		t.Fatal("two actual framed entries", err)
	}
	for i, fn := range owner.owner.functions {
		if fn.info.Entry != functions[i].info.Entry || fn.info.CodeBytes != functions[i].info.CodeBytes || fn.info.FrameBytes != functions[i].info.FrameBytes {
			t.Fatal("derived function span or frame", i, fn.info, functions[i].info)
		}
	}
	for _, entries := range [][]int{{second, 0}, {0, 0}, {0, 16}, {0, second + 16}, {0, second, second + 16}} {
		if rejected, err := NewNativeTailDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, entries, signatures); rejected != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid function order or span accepted", entries, err)
		}
	}
	if rejected, err := NewNativeTailDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, []int{0, second}, []wasm.CompType{signatures[1], signatures[0]}); rejected != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("reordered signatures accepted", err)
	}
}
