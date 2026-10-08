//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func tailDynamicOwnerForTest(t *testing.T, functions []tailRAFunction) tailRADynamicOwner {
	t.Helper()
	ar, err := coreruntime.NewArena(40*(len(functions)+1) + 32 + 32*len(functions))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	o := tailRADynamicOwner{descriptors: ar.Alloc(40 * (len(functions) + 1)), directory: ar.Alloc(32 + 32*len(functions)), context: functions[0].context, functions: append([]tailRAFunction(nil), functions...)}
	anchor, ctx, dir := tailRAPointer(o.descriptors), tailRAPointer(o.context), tailRAPointer(o.directory)
	binary.LittleEndian.PutUint64(o.context[16:], uint64(anchor))
	binary.LittleEndian.PutUint64(o.descriptors[8:], uint64(dir))
	binary.LittleEndian.PutUint64(o.descriptors[16:], uint64(functions[0].job.LinMemBase()))
	binary.LittleEndian.PutUint64(o.descriptors[32:], uint64(ctx))
	binary.LittleEndian.PutUint64(o.directory, abi.RV64TailTargetsMagic)
	binary.LittleEndian.PutUint32(o.directory[8:], abi.RV64TailTargetsVersion)
	binary.LittleEndian.PutUint32(o.directory[12:], uint32(len(functions)))
	binary.LittleEndian.PutUint64(o.directory[16:], uint64(anchor))
	binary.LittleEndian.PutUint64(o.directory[24:], uint64(ctx))
	for i := range o.functions {
		f := &o.functions[i]
		f.directory = o.descriptors
		entry, home := f.code.Base()+uintptr(f.info.Entry), f.job.LinMemBase()
		for n, v := range []uint64{uint64(entry), wasm.StructuralFuncTypeKey(f.signature), uint64(home) | abi.FuncRefLocalWrapperHomeTag, uint64(anchor + uintptr(40*(i+1))), uint64(ctx)} {
			binary.LittleEndian.PutUint64(o.descriptors[40*(i+1)+8*n:], v)
		}
		for n, v := range []uint64{uint64(entry), uint64(home), uint64(ctx), 0} {
			binary.LittleEndian.PutUint64(o.directory[32+32*i+8*n:], v)
		}
	}
	return o
}

func tailDynamicSiteForTest(t *testing.T, caller tailRAFunction, owners []tailRADynamicOwner, referenceBase int) NativeTailSite {
	t.Helper()
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		t.Fatal(err)
	}
	return NativeTailSite{dynamic: &tailRADynamicSite{referenceBase: referenceBase, key: wasm.StructuralFuncTypeKey(caller.signature), owners: owners}, CallerHome: caller.job.LinMemBase(), CallerContext: caller.context, TargetContext: caller.context, Target: NativeImportTarget{Entry: caller.code.Base() + uintptr(caller.info.Entry), Home: caller.job.LinMemBase(), Context: tailRAPointer(caller.context), HostTrampoline: trampoline, InvocationContext: true, StackOwnedActivation: true}, FrameBytes: caller.info.FrameBytes, SourceBase: callFrameHeader, SourceStride: 16, FunctionIndex: caller.info.mode.functionIndex, SourcePC: 37, Parameters: caller.signature.Params, Results: caller.signature.Results, Continuations: []NativeTailCodeRange{{caller.code.Base(), uintptr(len(caller.snapshot))}}}
}

func TestTailDynamicAdmissionAndGeneratedBudget(t *testing.T) {
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.F32}}
	f := tailRAProofFunctionFixture(t, sig, abi.RV64InstanceContextBytes)
	o := tailDynamicOwnerForTest(t, []tailRAFunction{f})
	f = o.functions[0]
	valid := tailDynamicSiteForTest(t, f, []tailRADynamicOwner{o}, f.info.FrameBytes-8)
	bound, err := nativeTailDispatcherBound([]NativeTailSite{valid}, len(f.snapshot))
	if err != nil {
		t.Fatal(err)
	}
	p := nativeTailProofSite(valid)
	pre, err := tailRAPreflight(p, 0x20000, 0x30000)
	if err != nil {
		t.Fatal(err)
	}
	final, err := tailRAFinalize(p, 0x20000)
	if err != nil || len(pre)+len(final)+32 > bound {
		t.Fatal("generated bound", len(pre), len(final), bound, err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*NativeTailSite)
	}{
		{"missing-owner", func(s *NativeTailSite) { s.dynamic.owners = nil }},
		{"count-before-copy", func(s *NativeTailSite) { s.dynamic.owners = make([]tailRADynamicOwner, tailRAMaxRanges+1) }},
		{"unaligned-reference", func(s *NativeTailSite) { s.dynamic.referenceBase-- }},
		{"header-reference", func(s *NativeTailSite) { s.dynamic.referenceBase = 40 }},
		{"outside-reference", func(s *NativeTailSite) { s.dynamic.referenceBase = s.FrameBytes }},
		{"argument-reference", func(s *NativeTailSite) { s.dynamic.referenceBase = 48 }},
		{"swar-zero-half-reference", func(s *NativeTailSite) { s.dynamic.referenceBase = 56 }},
		{"wrong-signature", func(s *NativeTailSite) { s.dynamic.key++ }},
		{"ambiguous-owner", func(s *NativeTailSite) { s.dynamic.owners = append(s.dynamic.owners, s.dynamic.owners[0]) }},
		{"directory-extent", func(s *NativeTailSite) { s.dynamic.owners[0].directory = s.dynamic.owners[0].directory[:32] }},
		{"descriptor-extent", func(s *NativeTailSite) { s.dynamic.owners[0].descriptors = s.dynamic.owners[0].descriptors[:40] }},
		{"context-extent", func(s *NativeTailSite) { s.dynamic.owners[0].context = s.dynamic.owners[0].context[:128] }},
		{"host-mode", func(s *NativeTailSite) { s.HostEntry = true }},
		{"target-count", func(s *NativeTailSite) { s.dynamic.owners[0].targets = []tailRAFunction{} }},
		{"proxy-count", func(s *NativeTailSite) { s.dynamic.owners[0].proxies = []*tailRADynamicProxy{} }},
		{"proxy-without-targets", func(s *NativeTailSite) { s.dynamic.owners[0].proxies = []*tailRADynamicProxy{nil} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			s.dynamic = valid.dynamic.freeze()
			tc.mutate(&s)
			owner, err := NewNativeTailDispatcher([]NativeTailSite{s}, len(f.snapshot))
			if owner != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid input reached allocation/publication", err)
			}
			if binary.LittleEndian.Uint64(f.context[120:]) != 0 {
				t.Fatal("failed factory bound capability")
			}
		})
	}
	if _, err := nativeTailDispatcherBound([]NativeTailSite{valid}, MaxCodeBytes-bound+1); !errors.Is(err, ErrUnsupported) {
		t.Fatal("combined dynamic code budget", err)
	}
	if _, err := nativeTailDispatcherBound([]NativeTailSite{valid}, len(f.snapshot)-1); !errors.Is(err, ErrUnsupported) {
		t.Fatal("understated retained guest code", err)
	}
	frozen := valid.dynamic.freeze()
	valid.dynamic.owners[0].functions[0].signature.Params[0] = wasm.F32
	if !wasm.EqualValType(frozen.owners[0].functions[0].signature.Params[0], wasm.I32) {
		t.Fatal("mutable signature shared")
	}
	valid.dynamic.owners[0].functions[0].info.Entry += 16
	if frozen.owners[0].functions[0].info.Entry != f.info.Entry {
		t.Fatal("mutable function metadata shared")
	}
}
