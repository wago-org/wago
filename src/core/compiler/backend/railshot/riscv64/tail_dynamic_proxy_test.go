//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func tailDynamicProxyForTest(t *testing.T, source, target tailRAFunction, index uint32) (tailRAFunction, *tailRADynamicProxy) {
	t.Helper()
	ar, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	p := &tailRADynamicProxy{importIndex: index, arguments: ar.Alloc(2048), results: ar.Alloc(2048)}
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		t.Fatal(err)
	}
	code, err := NativeImportThunk(NativeImportTarget{Entry: target.code.Base() + uintptr(target.info.Entry), Home: target.job.LinMemBase(), Context: tailRAPointer(target.context), Arguments: p.arguments, Results: p.results, ArgumentSlots: swarSignatureSlots(target.signature.Params), ResultSlots: swarSignatureSlots(target.signature.Results), StackOwnedActivation: true, InvocationContext: true, HostTrampoline: trampoline}, source.job.LinMemBase(), tailRAPointer(source.context), index)
	if err != nil {
		t.Fatal(err)
	}
	return tailRAFunction{code: tailRAProofBuffer(t, code), snapshot: bytes.Clone(code), signature: target.signature, info: FunctionInfo{Entry: 0, CodeBytes: len(code)}, job: source.job, context: source.context, directory: source.directory}, p
}

func tailDynamicProxyRowsForTest(o *tailRADynamicOwner, targets []tailRAFunction, proxies []*tailRADynamicProxy) {
	o.targets = append([]tailRAFunction(nil), targets...)
	o.proxies = append([]*tailRADynamicProxy(nil), proxies...)
	for i, f := range o.targets {
		for n, v := range []uint64{uint64(f.code.Base() + uintptr(f.info.Entry)), uint64(f.job.LinMemBase()), uint64(tailRAPointer(f.context)), 0} {
			binary.LittleEndian.PutUint64(o.directory[32+32*i+8*n:], v)
		}
	}
}

func TestTailDynamicProxyAdmissionBeforePublication(t *testing.T) {
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.F32}}
	a := tailRAProofFunctionFixture(t, sig, abi.RV64InstanceContextBytes)
	b := tailRAProofFunctionFixture(t, sig, abi.RV64InstanceContextBytes)
	proxy, proof := tailDynamicProxyForTest(t, a, b, 0)
	oa := tailDynamicOwnerForTest(t, []tailRAFunction{a, proxy})
	ob := tailDynamicOwnerForTest(t, []tailRAFunction{b})
	a, b = oa.functions[0], ob.functions[0]
	tailDynamicProxyRowsForTest(&oa, []tailRAFunction{a, b}, []*tailRADynamicProxy{nil, proof})
	valid := tailDynamicSiteForTest(t, a, []tailRADynamicOwner{oa}, a.info.FrameBytes-8)
	guest := len(a.snapshot) + len(b.snapshot) + len(proxy.snapshot)
	if _, err := nativeTailDispatcherBound([]NativeTailSite{valid}, guest); err != nil {
		t.Fatal("authentic ordinary proxy control", err)
	}
	if _, err := nativeTailDispatcherBound([]NativeTailSite{valid}, guest-1); !errors.Is(err, ErrUnsupported) {
		t.Fatal("true producer image not charged", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*NativeTailSite)
	}{
		{"target-count", func(s *NativeTailSite) { s.dynamic.owners[0].targets = s.dynamic.owners[0].targets[:1] }},
		{"proxy-count", func(s *NativeTailSite) { s.dynamic.owners[0].proxies = s.dynamic.owners[0].proxies[:1] }},
		{"missing-proxy-proof", func(s *NativeTailSite) { s.dynamic.owners[0].proxies[1] = nil }},
		{"wrong-import-index", func(s *NativeTailSite) { s.dynamic.owners[0].proxies[1].importIndex = 1 }},
		{"oversized-import-index", func(s *NativeTailSite) { s.dynamic.owners[0].proxies[1].importIndex = MaxHostFunctionImports }},
		{"argument-storage", func(s *NativeTailSite) {
			s.dynamic.owners[0].proxies[1].arguments = s.dynamic.owners[0].proxies[1].arguments[:8]
		}},
		{"result-storage", func(s *NativeTailSite) {
			s.dynamic.owners[0].proxies[1].results = s.dynamic.owners[0].proxies[1].results[:8]
		}},
		{"wrong-proxy-span", func(s *NativeTailSite) { s.dynamic.owners[0].functions[1].info.CodeBytes -= 4 }},
		{"wrong-source-signature", func(s *NativeTailSite) { s.dynamic.owners[0].functions[1].signature.Params[0] = wasm.F32 }},
		{"wrong-producer-signature", func(s *NativeTailSite) { s.dynamic.owners[0].targets[1].signature.Results[0] = wasm.I32 }},
		{"bridge-as-producer", func(s *NativeTailSite) { s.dynamic.owners[0].targets[1] = s.dynamic.owners[0].functions[1] }},
		{"truncated-producer-context", func(s *NativeTailSite) {
			s.dynamic.owners[0].targets[1].context = s.dynamic.owners[0].targets[1].context[:128]
		}},
		{"retarget-defined-descriptor", func(s *NativeTailSite) { s.dynamic.owners[0].targets[0] = s.dynamic.owners[0].targets[1] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			s.dynamic = valid.dynamic.freeze()
			tc.mutate(&s)
			o, err := NewNativeTailDispatcher([]NativeTailSite{s}, guest)
			if o != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid proxy reached publication", err)
			}
			if binary.LittleEndian.Uint64(a.context[120:]) != 0 {
				t.Fatal("failed proxy bound capability")
			}
		})
	}
	frozen := valid.dynamic.freeze()
	valid.dynamic.owners[0].proxies[1].importIndex++
	if frozen.owners[0].proxies[1].importIndex != 0 {
		t.Fatal("mutable proxy proof metadata shared")
	}
}
