//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func tailRAProofBuffer(t *testing.T, code []byte) *coreruntime.CodeBuffer {
	t.Helper()
	b, err := coreruntime.NewCodeBuffer(len(code))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := b.Append(code); err != nil {
		t.Fatal(err)
	}
	return b
}

func tailRAProofFunctionFixture(t *testing.T, sig *wasm.CompType, contextBytes int) tailRAFunction {
	t.Helper()
	j, err := coreruntime.NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := j.Close(); err != nil {
			t.Error(err)
		}
	})
	ar, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, dir := ar.Alloc(contextBytes), ar.Alloc(40)
	binary.LittleEndian.PutUint64(ctx[16:], uint64(tailRAPointer(dir)))
	binary.LittleEndian.PutUint64(dir[32:], uint64(tailRAPointer(ctx)))
	var a enc.Asm
	for cell := 0; cell < swarSignatureSlots(sig.Results); cell++ {
		a.Sd(enc.Zero, enc.X13, int32(cell*8))
	}
	widths := make([]bool, 2*len(sig.Params)+2)
	for i := range widths {
		widths[i] = true
	}
	mode := emissionMode{production: true, interruptible: true, entryPC: 1}
	info := FunctionInfo{FrameHeaderBytes: callFrameHeader, Params: swarSignatureSlots(sig.Params), Results: swarSignatureSlots(sig.Results), mode: mode}
	code, info, err := frameFunctionCalls(a.B, widths, swarSignatureSlots(sig.Params), 0, true, -1, info, nil)
	if err != nil {
		t.Fatal(err)
	}
	info.CodeBytes = len(code)
	return tailRAFunction{code: tailRAProofBuffer(t, code), snapshot: append([]byte(nil), code...), info: info, signature: sig, job: j, context: ctx, directory: dir}
}

func tailRAOwnerFixture(t *testing.T, params, results []wasm.ValType) tailRAOwnerSpec {
	t.Helper()
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: results}
	c := tailRAProofFunctionFixture(t, sig, tailRAContextBytes)
	target := tailRAProofFunctionFixture(t, sig, coreruntime.InstanceContextBytes)
	exit, err := tailRAExitCode()
	if err != nil {
		t.Fatal(err)
	}
	return tailRAOwnerSpec{sites: []tailRAOwnerSite{{caller: c, target: target, sourcePC: 41, sourceBase: callFrameHeader, sourceStride: 16}}, exit: tailRAProofBuffer(t, exit), arguments: []tailRARange{{0x10000, 2048}}, results: []tailRARange{{0x20000, 2048}}, continuations: []tailRARange{{0x30004, 4}}, retained: []any{c.code, target.code, c.job, target.job}, stackLimit: 0x100000, stackTop: 0x180000, hostTrampoline: 0x40000}
}

func TestTailRAProofExactMaskAndFullPhysicalSignature(t *testing.T) {
	if tailRARecordBytes != 5504 {
		t.Fatal("independent record extent")
	}
	if tailRACtrlOffset != 160 || tailRAArgsOffset != tailRACtrlOffset+HostFixedFrameBytes || tailRAResultsOffset != tailRAArgsOffset+256*8 || tailRARecordBytes != tailRAResultsOffset+256*8 || tailRAContextBytes != coreruntime.InstanceContextBytes+8 {
		t.Fatal("independent ABI layout")
	}
	for _, offset := range []int{tailRACtrlOffset, tailRAArgsOffset, tailRAResultsOffset, tailRARecordBytes} {
		if offset&15 != 0 {
			t.Fatal("unaligned region", offset)
		}
	}
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128}
	mask, count, err := tailRAResultMask(types)
	if err != nil || count != 6 || mask != [4]uint64{5, 0, 0, 0} {
		t.Fatal("independent exact cell mask", mask, count, err)
	}
	for _, typ := range []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.V128} {
		n := 256
		if wasm.EqualValType(typ, wasm.V128) {
			n = 128
		}
		list := make([]wasm.ValType, n)
		for i := range list {
			list[i] = typ
		}
		mask, count, err := tailRAResultMask(list)
		if err != nil || count != 256 {
			t.Fatal("physical256", typ, err)
		}
		want := [4]uint64{}
		if wasm.EqualValType(typ, wasm.I32) || wasm.EqualValType(typ, wasm.F32) {
			want = [4]uint64{^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)}
		}
		if mask != want {
			t.Fatal("full mask", typ, mask)
		}
		if _, _, err := tailRAResultMask(append(list, typ)); !errors.Is(err, ErrUnsupported) {
			t.Fatal("next signature", typ, err)
		}
	}
	if _, _, err := tailRAResultMask([]wasm.ValType{wasm.FuncRef}); !errors.Is(err, ErrUnsupported) {
		t.Fatal("reference results widened", err)
	}
}

func TestTailRAProofAdmissionBeforeAnyOwnerAllocation(t *testing.T) {
	base := tailRAOwnerFixture(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	for _, mutate := range []func(*tailRAOwnerSpec){
		func(s *tailRAOwnerSpec) { s.sites = nil },
		func(s *tailRAOwnerSpec) { s.sites = make([]tailRAOwnerSite, tailRAMaxSites+1) },
		func(s *tailRAOwnerSpec) { s.sites[0].caller.info.FrameHeaderBytes = 16 },
		func(s *tailRAOwnerSpec) { s.sites[0].caller.info.mode.production = false },
		func(s *tailRAOwnerSpec) { s.sites[0].caller.info.FrameBytes = MaxFrameBytes + 16 },
		func(s *tailRAOwnerSpec) {
			s.sites[0].target.signature = &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.F32}}
		},
		func(s *tailRAOwnerSpec) { s.sites[0].sourceBase = 0 },
		func(s *tailRAOwnerSpec) { s.sites[0].sourceStride = 7 },
		func(s *tailRAOwnerSpec) {
			s.sites[0].caller.context = s.sites[0].caller.context[:coreruntime.InstanceContextBytes]
		},
		func(s *tailRAOwnerSpec) { s.arguments = nil },
		func(s *tailRAOwnerSpec) { s.results = nil },
		func(s *tailRAOwnerSpec) { s.continuations = []tailRARange{{0x30001, 4}} },
		func(s *tailRAOwnerSpec) { s.continuations = []tailRARange{{^uintptr(0) - 3, 8}} },
		func(s *tailRAOwnerSpec) { s.hostTrampoline = 1 },
		func(s *tailRAOwnerSpec) { s.retained = nil },
		func(s *tailRAOwnerSpec) { s.stackTop = s.stackLimit },
	} {
		s := base
		s.sites = append([]tailRAOwnerSite(nil), base.sites...)
		mutate(&s)
		allocations := 0
		allocate := func(int) (*coreruntime.Arena, error) { allocations++; return nil, errors.New("unexpected arena") }
		mapCode := func(int) (*coreruntime.CodeBuffer, error) { allocations++; return nil, errors.New("unexpected code") }
		if o, err := newTailRAOwnerWith(s, allocate, mapCode); !errors.Is(err, ErrUnsupported) || o != nil || allocations != 0 {
			t.Fatal("preflight allocated/staged", err, allocations)
		}
	}
	for _, n := range []int{maxDirectCalls, maxDirectCalls + 1} {
		s := base
		s.sites = make([]tailRAOwnerSite, n)
		for i := range s.sites {
			s.sites[i] = base.sites[0]
		}
		_, _, err := tailRAOwnedBound(s)
		if n == maxDirectCalls && err != nil || n > maxDirectCalls && !errors.Is(err, ErrUnsupported) {
			t.Fatal("site boundary", n, err)
		}
	}
}

func TestTailRAProofZeroCellNilOriginalArenasAndLeaseCleanup(t *testing.T) {
	s := tailRAOwnerFixture(t, nil, nil)
	s.arguments, s.results = nil, nil
	o, err := newTailRAOwner(s)
	if err != nil {
		if o != nil {
			_ = o.Close()
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := o.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := o.acquire(); err != nil {
		t.Fatal(err)
	}
	cap := binary.LittleEndian.Uint64(s.sites[0].caller.context[tailRACapOffset:])
	if cap == 0 || binary.LittleEndian.Uint32(o.capability[12:]) != 1 {
		t.Fatal("publication")
	}
	if err := o.Close(); !errors.Is(err, ErrUnsupported) || binary.LittleEndian.Uint64(s.sites[0].caller.context[tailRACapOffset:]) != cap {
		t.Fatal("active lease retired", err)
	}
	// A cold exit never invokes a native exit destructor. The invocation owner
	// releases its lease explicitly after the engine has completed/decorated.
	o.release()
	fault := errors.New("cleanup retry")
	err = o.closeWith((*coreruntime.CodeBuffer).Close, func(*coreruntime.Arena) error { return fault })
	if !errors.Is(err, fault) || o.arena == nil || len(o.spec.retained) == 0 || binary.LittleEndian.Uint64(s.sites[0].caller.context[tailRACapOffset:]) != 0 {
		t.Fatal("detach/retain on cleanup failure", err)
	}
	if err := o.acquire(); !errors.Is(err, ErrUnsupported) {
		t.Fatal("detached owner callable", err)
	}
	if err := o.Close(); err != nil || o.arena != nil || len(o.spec.retained) != 0 {
		t.Fatal("cleanup retry", err)
	}
	if err := o.Close(); err != nil {
		t.Fatal("idempotent close", err)
	}
}

func TestTailRAProofGuestBytesHaveNoProcessAddresses(t *testing.T) {
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.V128}, Results: []wasm.ValType{wasm.F32}}
	guest := func() []byte {
		f := integerEmitter{target: target{header: callFrameHeader, mode: emissionMode{production: true, interruptible: true, functionIndex: 3}}, frameBytes: 128}
		f.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(callFrameHeader + 16*i) })
		if f.err != nil || f.a.Err() != nil {
			t.Fatal(f.err, f.a.Err())
		}
		return f.a.B
	}
	one, two := guest(), guest()
	if !bytes.Equal(one, two) {
		t.Fatal("guest process identity leaked")
	}
	for i := 0; i+4 <= len(one); i += 4 {
		word := binary.LittleEndian.Uint32(one[i:])
		op := word & 127
		rd := word >> 7 & 31
		if op == 0x13 || op == 0x03 || op == 0x33 || op == 0x17 || op == 0x67 {
			if rd == 3 || rd == 4 || rd == 26 || rd == 27 {
				t.Fatal("reserved Go register written", i, rd)
			}
		}
	}
	code, err := tailRAExitCode()
	if err != nil || len(code) > tailRADispatchBound || binary.LittleEndian.Uint32(code[:4]) != 0x00000497 {
		t.Fatal("address-free entry AUIPC x9", len(code), err)
	}
}

func TestTailRAProofFailedPublicationOwnerCannotAcquireAndRetriesCleanup(t *testing.T) {
	s := tailRAOwnerFixture(t, nil, nil)
	s.arguments, s.results = nil, nil
	fault := errors.New("publication failed before callable visibility")
	o, err := newTailRAOwnerPublishWith(s, coreruntime.NewArena, coreruntime.NewCodeBuffer, func(*coreruntime.CodeBuffer) error { return fault })
	if !errors.Is(err, fault) || o == nil || o.code == nil || o.arena == nil || o.ready || binary.LittleEndian.Uint64(s.sites[0].caller.context[120:]) != 0 {
		t.Fatal("failed publication state", err)
	}
	if err := o.acquire(); !errors.Is(err, ErrUnsupported) {
		t.Fatal("partial owner acquired", err)
	}
	cleanup := errors.New("mapping retirement failed")
	if err := o.closeWith(func(*coreruntime.CodeBuffer) error { return cleanup }, func(*coreruntime.Arena) error { return cleanup }); !errors.Is(err, cleanup) || o.code == nil || o.arena == nil {
		t.Fatal("failed retirement lost owned maps", err)
	}
	if err := o.acquire(); !errors.Is(err, ErrUnsupported) {
		t.Fatal("failed cleanup owner acquired", err)
	}
	if err := o.Close(); err != nil || o.code != nil || o.arena != nil {
		t.Fatal("retirement retry", err)
	}
}

func TestTailRAProofIndependentMetadataCodeAndRangeCapsBeforeStaging(t *testing.T) {
	// Wide numeric signatures isolate the metadata cap:128 sites are far below
	// the1024 per-function count and need only about1 MiB of dispatcher code.
	wide := make([]wasm.ValType, 256)
	for i := range wide {
		wide[i] = wasm.I64
	}
	metadata := tailRAOwnerFixture(t, wide, wide)
	if n, _, err := tailRAOwnedBound(metadata); err != nil || n >= MaxModuleBytes {
		t.Fatal("admitted wide baseline", n, err)
	}
	metadataOriginal := metadata.sites[0]
	metadata.sites = make([]tailRAOwnerSite, 128)
	for i := range metadata.sites {
		metadata.sites[i] = metadataOriginal
	}
	// Construct an independent scalar baseline for the range/combined-code cap.
	code := tailRAOwnerFixture(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64})
	original := code.sites[0]
	code.arguments = make([]tailRARange, tailRAMaxRanges)
	code.results = make([]tailRARange, tailRAMaxRanges)
	code.continuations = make([]tailRARange, tailRAMaxRanges)
	for i := 0; i < tailRAMaxRanges; i++ {
		code.arguments[i] = tailRARange{0x10000, 8}
		code.results[i] = tailRARange{0x20000, 8}
		code.continuations[i] = tailRARange{0x30004, 4}
	}
	if _, n, err := tailRAOwnedBound(code); err != nil || n >= MaxCodeBytes {
		t.Fatal("exact independent range-count boundary", n, err)
	}
	ranges := code
	ranges.continuations = append(append([]tailRARange(nil), code.continuations...), tailRARange{0x30004, 4})
	code.sites = make([]tailRAOwnerSite, 42)
	for i := range code.sites {
		code.sites[i] = original
	}
	for _, c := range []struct {
		name, reason string
		spec         tailRAOwnerSpec
	}{{"metadata", "metadata bound", metadata}, {"combined-dispatch-code", "combined dispatcher bound", code}, {"continuation-count", "counts/lease/stack", ranges}} {
		allocations := 0
		allocate := func(int) (*coreruntime.Arena, error) { allocations++; return nil, errors.New("unexpected arena") }
		mapCode := func(int) (*coreruntime.CodeBuffer, error) { allocations++; return nil, errors.New("unexpected code") }
		o, err := newTailRAOwnerWith(c.spec, allocate, mapCode)
		if !errors.Is(err, ErrUnsupported) || err == nil || !strings.Contains(err.Error(), c.reason) || o != nil || allocations != 0 {
			t.Fatal("independent bounded policy failed before staging", c.name, err, allocations)
		}
	}
}
