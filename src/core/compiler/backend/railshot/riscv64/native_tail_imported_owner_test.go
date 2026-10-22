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

func TestNativeTailImportedOwnerCertifiedIdentityAndBudgets(t *testing.T) {
	scalar, vector := make([]wasm.ValType, 256), make([]wasm.ValType, 128)
	for i := range scalar {
		scalar[i] = wasm.I32
	}
	for i := range vector {
		vector[i] = wasm.V128
	}
	for _, types := range [][]wasm.ValType{{wasm.I32}, {wasm.F32, wasm.V128, wasm.I64}, scalar, vector} {
		sig := wasm.CompType{Kind: wasm.CompFunc, Params: append([]wasm.ValType(nil), types...), Results: append([]wasm.ValType(nil), types...)}
		source := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
		target := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
		binary.LittleEndian.PutUint64(target.directory[16:], uint64(target.job.LinMemBase()))
		proxy, proof := tailDynamicProxyForTest(t, source, target, 0)
		o := tailDynamicOwnerForTest(t, []tailRAFunction{proxy, source})
		source, proxy = o.functions[1], o.functions[0]
		tailDynamicProxyRowsForTest(&o, []tailRAFunction{target, source}, []*tailRADynamicProxy{proof, nil})
		trampoline, err := coreruntime.HostCallTrampolinePointer()
		if err != nil {
			t.Fatal(err)
		}
		callee := NativeImportTarget{Entry: target.codeBase(), Home: target.job.LinMemBase(), Context: tailRAPointer(target.context), Arguments: proof.arguments, Results: proof.results, ArgumentSlots: swarSignatureSlots(types), ResultSlots: swarSignatureSlots(types), StackOwnedActivation: true, InvocationContext: true, HostTrampoline: trampoline}
		bridge, cert, err := NativeImportThunkWithCertificate(callee, source.job.LinMemBase(), tailRAPointer(source.context), 0)
		if err != nil || !bytes.Equal(bridge, proxy.snapshot) {
			t.Fatal("exact issued bridge", err)
		}
		source, sourceLease := tailPublishedFunctionForTest(t, source)
		proxy, proxyLease := tailPublishedFunctionForTest(t, proxy)
		target, targetLease := tailPublishedFunctionForTest(t, target)
		genuine, err := NewNativeTailFunction(target.published, target.job, target.context, target.directory, target.info.Entry, target.info.Entry+target.info.CodeBytes, 0, sig)
		if err != nil || len(genuine.function.directory) != 40 {
			t.Fatal("genuine descriptor-zero-only producer", err)
		}
		imports := []NativeTailImport{{Image: proxy.published, Offset: 0, CodeBytes: len(bridge), Target: genuine, Arguments: proof.arguments, Results: proof.results, Certificate: cert}}
		signatures := []wasm.CompType{sig, sig}
		build := func() (*NativeTailOwner, error) {
			return NewNativeTailImportedOwner(source.published, source.job, o.context, o.descriptors, o.directory, []int{0}, signatures, imports, trampoline)
		}
		owner, err := build()
		if err != nil {
			t.Fatal(err)
		}
		if owner.owner.functions[0].job != source.job || owner.owner.targets[0].job != target.job || owner.owner.functions[1].info.mode.functionIndex != 1 {
			t.Fatal("importer identity or full function index lost")
		}
		if a := testing.AllocsPerRun(4, func() {
			if err := owner.Validate(); err != nil {
				panic(err)
			}
		}); a != 0 {
			t.Fatal("warm imported owner allocated", a)
		}
		for _, sites := range []int{1, 4} {
			values := 4 * len(types)
			counts := NativeTailOwnerCounts{Owners: 1, Functions: 2, Values: values, Targets: 2, TargetValues: values, ProxySlots: 2, Proxies: 1}
			guest := len(source.snapshot) + len(target.snapshot) + len(proxy.snapshot)
			m, c, err := NativeTailFactoryCountsBudget(sites, 2*len(types)*sites, 3, counts, guest)
			am, ac, actualErr := NativeTailDefinedFactoryBudget(sites, 2*len(types)*sites, 3, []*NativeTailOwner{owner}, guest)
			if err != nil || actualErr != nil || m != am || c != ac {
				t.Fatal("count-only budget differs from frozen structures", m, am, c, ac, err, actualErr)
			}
		}
		for _, mutation := range []string{"target", "certificate", "span", "transport", "trampoline", "signature", "retarget", "lease-source", "lease-proxy", "lease-target"} {
			t.Run(mutation, func(t *testing.T) {
				saved := imports[0]
				originalTrampoline := trampoline
				originalSignatures := signatures
				row := bytes.Clone(o.directory[32:64])
				switch mutation {
				case "target":
					imports[0].Target = NativeTailFunction{}
				case "certificate":
					imports[0].Certificate = nil
				case "span":
					imports[0].CodeBytes -= 4
				case "transport":
					imports[0].Arguments = imports[0].Results
				case "trampoline":
					trampoline += 4
				case "signature":
					signatures = []wasm.CompType{{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}}, sig}
				case "retarget":
					binary.LittleEndian.PutUint64(o.directory[32:], uint64(source.codeBase()))
				case "lease-source":
					sourceLease.err = ErrUnsupported
				case "lease-proxy":
					proxyLease.err = ErrUnsupported
				case "lease-target":
					targetLease.err = ErrUnsupported
				}
				if invalid, err := build(); invalid != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("invalid imported owner accepted", err)
				}
				imports[0], trampoline, signatures = saved, originalTrampoline, originalSignatures
				copy(o.directory[32:], row)
				sourceLease.err, proxyLease.err, targetLease.err = nil, nil, nil
			})
		}
		// Deep mutation must not alter either the wrapper or genuine target signatures.
		sig.Params[0], sig.Results[0] = wasm.FuncRef, wasm.ExternRef
		if err := owner.Validate(); err != nil {
			t.Fatal("deep signature mutation escaped opaque ownership", err)
		}
		// Snapshot metadata is immutable even if the runtime reuses its inputs.
		signatures[0] = wasm.CompType{}
		imports[0] = NativeTailImport{}
		if err := owner.Validate(); err != nil {
			t.Fatal("input mutation escaped opaque ownership", err)
		}
		owner.trampoline += 4
		if err := owner.Validate(); !errors.Is(err, ErrUnsupported) {
			t.Fatal("owner did not retain exact trampoline", err)
		}
		for _, lease := range []*tailPublishedOwnerForTest{sourceLease, proxyLease, targetLease} {
			lease.err = errors.New("retired particular owner")
			if err := owner.Validate(); !errors.Is(err, lease.err) {
				t.Fatal("warm validation missed retired physical owner", err)
			}
			lease.err = nil
		}
	}
}

func TestNativeTailImportedOwnerRejectsUnboundedCounts(t *testing.T) {
	for _, counts := range [][3]int{{0, 0, 0}, {1, 2, 0}, {2, 0, 0}, {MaxFunctions + 1, 1, 0}, {2, 1, 1025}, {2, 1, -1}} {
		if _, err := NativeTailImportedOwnerBytes(counts[0], counts[1], counts[2]); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid owner counts", counts, err)
		}
	}
	for _, counts := range []NativeTailOwnerCounts{{Owners: 1, Functions: 2, Targets: 3}, {Owners: 1, Functions: 2, Targets: 1, TargetValues: 513}, {Owners: 1, Functions: 2, Targets: 1, ProxySlots: 2}, {Owners: 1, Functions: 2, Targets: 1, ProxySlots: 1, Proxies: 2}} {
		if _, _, err := NativeTailFactoryCountsBudget(1, 0, 1, counts, 4); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid factory counts", counts, err)
		}
	}
}
