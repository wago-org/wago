//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestNativeTailHostOwnerIdentityAndBudgets(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, count := range []int{0, 4, 64} {
			t.Run(fmt.Sprintf("owned%t-values%d", owned, count), func(t *testing.T) {
				types := make([]wasm.ValType, count)
				numeric := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
				for i := range types {
					types[i] = numeric[i%len(numeric)]
				}
				sig := wasm.CompType{Kind: wasm.CompFunc, Params: types, Results: append([]wasm.ValType(nil), types...)}
				source := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
				code, cert, err := NativeTailHostThunkWithCertificate(37, count, count, owned)
				if err != nil {
					t.Fatal(err)
				}
				host := tailRAFunction{code: tailRAProofBuffer(t, code), snapshot: code, signature: &sig, job: source.job, context: source.context, directory: source.directory, info: FunctionInfo{CodeBytes: len(code)}}
				host, hostLease := tailPublishedFunctionForTest(t, host)
				o := tailDynamicOwnerForTest(t, []tailRAFunction{host, source})
				host, source = o.functions[0], o.functions[1]
				source, _ = tailPublishedFunctionForTest(t, source)
				binary.LittleEndian.PutUint64(o.directory[32+24:], abi.RV64TailTargetsHost)
				h, err := NewNativeTailHostFunction(host.published, cert, source.job, o.context, o.descriptors, 0, 37, owned, sig)
				if err != nil {
					t.Fatal(err)
				}
				trampoline, err := coreruntime.HostCallTrampolinePointer()
				if err != nil {
					t.Fatal(err)
				}
				signatures := []wasm.CompType{sig, sig}
				imports := []NativeTailImport{{Image: host.published, Offset: 0, CodeBytes: len(code), Host: h}}
				build := func() (*NativeTailOwner, error) {
					return NewNativeTailImportedOwner(source.published, source.job, o.context, o.descriptors, o.directory, []int{0}, signatures, imports, trampoline)
				}
				owner, err := build()
				if err != nil {
					t.Fatal(err)
				}
				if owner.owner.host(0) != h || owner.owner.proxy(0) != nil || owner.owner.host(1) != nil {
					t.Fatal("host/guest target classification")
				}
				if n := testing.AllocsPerRun(4, func() {
					if err := owner.Validate(); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatal("warm host owner validation allocation", n)
				}
				sites := 3
				counts := NativeTailOwnerCounts{Owners: 1, Functions: 2, Values: 4 * count, Targets: 2, TargetValues: 4 * count, ProxySlots: 2, HostSlots: 2}
				am, ac, err := NativeTailDefinedFactoryBudget(sites, 2*count*sites, 2, []*NativeTailOwner{owner}, len(source.snapshot)+len(code))
				cm, cc, cerr := NativeTailFactoryCountsBudget(sites, 2*count*sites, 2, counts, len(source.snapshot)+len(code))
				if err != nil || cerr != nil || am != cm || ac != cc {
					t.Fatal("host counted/frozen metadata mismatch", am, cm, ac, cc, err, cerr)
				}
				base, err := NativeTailImportedOwnerBytes(2, 1, 4*count)
				withHosts, herr := NativeTailHostOwnerBytes(2, 1, 4*count)
				if err != nil || herr != nil || withHosts <= base {
					t.Fatal("host directory staging charge", base, withHosts, err, herr)
				}
				for _, name := range []string{"missing-host", "mixed-native-proof", "wrong-host-index", "wrong-span", "wrong-mode", "signature", "host-flag", "defined-flag"} {
					t.Run(name, func(t *testing.T) {
						saved := imports[0]
						hcopy := *h
						imports[0].Host = &hcopy
						defer func() {
							imports[0] = saved
							binary.LittleEndian.PutUint64(o.directory[32+24:], abi.RV64TailTargetsHost)
							binary.LittleEndian.PutUint64(o.directory[64+24:], 0)
						}()
						switch name {
						case "missing-host":
							imports[0].Host = nil
						case "mixed-native-proof":
							imports[0].Certificate = &NativeImportCertificate{}
						case "wrong-host-index":
							hcopy.dispatchIndex++
						case "wrong-span":
							imports[0].CodeBytes -= 4
						case "wrong-mode":
							hcopy.owned = !hcopy.owned
						case "signature":
							hcopy.signature = &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}
						case "host-flag":
							binary.LittleEndian.PutUint64(o.directory[32+24:], 0)
						case "defined-flag":
							binary.LittleEndian.PutUint64(o.directory[64+24:], abi.RV64TailTargetsHost)
						}
						got, err := build()
						if got != nil || !errors.Is(err, ErrUnsupported) {
							t.Fatal("unqualified host owner accepted", err)
						}
					})
				}
				hostLease.err = errors.New("retired host code owner")
				if err := owner.Validate(); !errors.Is(err, hostLease.err) {
					t.Fatal("host target outlived wrapper owner", err)
				}
			})
		}
	}
}

func TestNativeTailHostOwnerMixedCertifiedProxy(t *testing.T) {
	sig := wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}, Results: []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}}
	source := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
	target := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
	binary.LittleEndian.PutUint64(target.directory[16:], uint64(target.job.LinMemBase()))
	proxy, proof := tailDynamicProxyForTest(t, source, target, 1)
	code, hostCert, err := NativeTailHostThunkWithCertificate(37, 4, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	host := tailRAFunction{code: tailRAProofBuffer(t, code), snapshot: code, signature: &sig, job: source.job, context: source.context, directory: source.directory, info: FunctionInfo{CodeBytes: len(code)}}
	o := tailDynamicOwnerForTest(t, []tailRAFunction{host, proxy, source})
	host, proxy, source = o.functions[0], o.functions[1], o.functions[2]
	tailDynamicProxyRowsForTest(&o, []tailRAFunction{host, target, source}, []*tailRADynamicProxy{nil, proof, nil})
	binary.LittleEndian.PutUint64(o.directory[32+24:], abi.RV64TailTargetsHost)
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		t.Fatal(err)
	}
	callee := NativeImportTarget{Entry: target.codeBase(), Home: target.job.LinMemBase(), Context: tailRAPointer(target.context), Arguments: proof.arguments, Results: proof.results, ArgumentSlots: 4, ResultSlots: 4, StackOwnedActivation: true, InvocationContext: true, HostTrampoline: trampoline}
	_, nativeCert, err := NativeImportThunkWithCertificate(callee, source.job.LinMemBase(), tailRAPointer(source.context), 1)
	if err != nil {
		t.Fatal(err)
	}
	source, _ = tailPublishedFunctionForTest(t, source)
	proxy, _ = tailPublishedFunctionForTest(t, proxy)
	target, _ = tailPublishedFunctionForTest(t, target)
	host, _ = tailPublishedFunctionForTest(t, host)
	genuine, err := NewNativeTailFunction(target.published, target.job, target.context, target.directory, 0, len(target.snapshot), 0, sig)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewNativeTailHostFunction(host.published, hostCert, source.job, o.context, o.descriptors, 0, 37, false, sig)
	if err != nil {
		t.Fatal(err)
	}
	imports := []NativeTailImport{{Image: host.published, CodeBytes: len(code), Host: h}, {Image: proxy.published, CodeBytes: len(proxy.snapshot), Target: genuine, Arguments: proof.arguments, Results: proof.results, Certificate: nativeCert}}
	owner, err := NewNativeTailImportedOwner(source.published, source.job, o.context, o.descriptors, o.directory, []int{0}, []wasm.CompType{sig, sig, sig}, imports, trampoline)
	if err != nil {
		t.Fatal(err)
	}
	if owner.owner.host(0) != h || owner.owner.proxy(0) != nil || owner.owner.host(1) != nil || owner.owner.proxy(1) == nil || owner.owner.host(2) != nil || owner.owner.proxy(2) != nil {
		t.Fatal("mixed host/proxy/defined classifications")
	}
	counts := NativeTailOwnerCounts{Owners: 1, Functions: 3, Values: 24, Targets: 3, TargetValues: 24, ProxySlots: 3, Proxies: 1, HostSlots: 3}
	guest := len(source.snapshot) + len(target.snapshot) + len(proxy.snapshot) + len(host.snapshot)
	m, c, err := NativeTailFactoryCountsBudget(1, 8, 4, counts, guest)
	am, ac, actualErr := NativeTailDefinedFactoryBudget(1, 8, 4, []*NativeTailOwner{owner}, guest)
	if err != nil || actualErr != nil || m != am || c != ac {
		t.Fatal("mixed owner byte budget", m, am, c, ac, err, actualErr)
	}
	caller := owner.owner.functions[2]
	site := tailDynamicSiteForTest(t, caller, nil, caller.info.FrameBytes-8)
	site.dynamic = nil
	site, err = NativeTailDefinedSite(site, caller.info.FrameBytes-8, []*NativeTailOwner{owner})
	if err != nil {
		t.Fatal(err)
	}
	frozen := site.dynamic.freeze()
	ps := nativeTailProofSite(site)
	if err := frozen.validate(&ps); err != nil {
		t.Fatal("mixed frozen factory validation", err)
	}
	if a := testing.AllocsPerRun(4, func() {
		if err := owner.Validate(); err != nil {
			panic(err)
		}
	}); a != 0 {
		t.Fatal("mixed warm validation allocation", a)
	}
	for _, row := range []int{0, 1, 2} {
		slot := o.directory[32+row*32+24:]
		original := binary.LittleEndian.Uint64(slot)
		binary.LittleEndian.PutUint64(slot, original^abi.RV64TailTargetsHost)
		if err := owner.Validate(); !errors.Is(err, ErrUnsupported) {
			t.Fatal("mixed target kind mutation accepted", row, err)
		}
		binary.LittleEndian.PutUint64(slot, original)
	}
}

func TestNativeTailHostOwnerRejectsUnboundedCounts(t *testing.T) {
	for _, slots := range []int{-1, 3} {
		counts := NativeTailOwnerCounts{Owners: 1, Functions: 2, HostSlots: slots}
		if _, _, err := NativeTailFactoryCountsBudget(1, 0, 1, counts, 4); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid host pointer count", slots, err)
		}
	}
	for _, counts := range [][3]int{{0, 0, 0}, {1, 2, 0}, {MaxFunctions + 1, 1, 0}, {2, 1, 1025}} {
		if _, err := NativeTailHostOwnerBytes(counts[0], counts[1], counts[2]); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid host owner count", counts, err)
		}
	}
}
