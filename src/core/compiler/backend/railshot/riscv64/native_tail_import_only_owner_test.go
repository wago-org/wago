//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestNativeTailImportOnlyOwnerCertifiedIdentity(t *testing.T) {
	for _, mode := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
		host, sparse := mode[0], mode[1]
		t.Run(fmt.Sprintf("host%t-sparse%t", host, sparse), func(t *testing.T) {
			sig := wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.F64}, Results: []wasm.ValType{wasm.I32, wasm.F64}}
			storage := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
			trampoline, err := coreruntime.HostCallTrampolinePointer()
			if err != nil {
				t.Fatal(err)
			}
			var wrapper tailRAFunction
			var importRow NativeTailImport
			var certificate *NativeTailHostCertificate
			codeBytes := 0
			if host {
				code, cert, err := NativeTailHostThunkWithCertificate(0, 2, 2, false)
				if err != nil {
					t.Fatal(err)
				}
				certificate = cert
				wrapper = tailRAFunction{code: tailRAProofBuffer(t, code), snapshot: code, signature: &sig, job: storage.job, context: storage.context, directory: storage.directory, info: FunctionInfo{CodeBytes: len(code)}}
			} else {
				target := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
				binary.LittleEndian.PutUint64(target.directory[16:], uint64(target.job.LinMemBase()))
				proxy, proof := tailDynamicProxyForTest(t, storage, target, 0)
				wrapper = proxy
				callee := NativeImportTarget{Entry: target.codeBase(), Home: target.job.LinMemBase(), Context: tailRAPointer(target.context), Arguments: proof.arguments, Results: proof.results, ArgumentSlots: 2, ResultSlots: 2, StackOwnedActivation: true, InvocationContext: true, HostTrampoline: trampoline}
				code, cert, err := NativeImportThunkWithCertificate(callee, storage.job.LinMemBase(), tailRAPointer(storage.context), 0)
				if err != nil || !bytes.Equal(code, proxy.snapshot) {
					t.Fatal("exact import-only native bridge", err)
				}
				target, _ = tailPublishedFunctionForTest(t, target)
				genuine, err := NewNativeTailFunction(target.published, target.job, target.context, target.directory, 0, len(target.snapshot), 0, sig)
				if err != nil {
					t.Fatal(err)
				}
				importRow = NativeTailImport{CodeBytes: len(code), Target: genuine, Arguments: proof.arguments, Results: proof.results, Certificate: cert}
				codeBytes = len(target.snapshot)
			}
			o := tailDynamicOwnerForTest(t, []tailRAFunction{wrapper})
			wrapper, lease := tailPublishedFunctionForTest(t, o.functions[0])
			importRow.Image = wrapper.published
			codeBytes += len(wrapper.snapshot)
			counts := NativeTailOwnerCounts{Owners: 1, Functions: 1, Values: 4, Targets: 1, TargetValues: 4, ProxySlots: 1}
			if host {
				binary.LittleEndian.PutUint64(o.directory[32+24:], abi.RV64TailTargetsHost)
				h, err := NewNativeTailHostFunction(wrapper.published, certificate, storage.job, o.context, o.descriptors, 0, 0, false, sig)
				if err != nil {
					t.Fatal(err)
				}
				importRow.Host, importRow.CodeBytes = h, certificate.CodeBytes()
				counts.HostSlots = 1
			} else {
				f := importRow.Target.function
				row := o.directory[32:]
				binary.LittleEndian.PutUint64(row, uint64(f.codeBase()))
				binary.LittleEndian.PutUint64(row[8:], uint64(f.job.LinMemBase()))
				binary.LittleEndian.PutUint64(row[16:], uint64(tailRAPointer(f.context)))
				counts.Proxies = 1
			}
			if sparse {
				counts.MaskSlots = 1
			}
			build := func(image *NativeTailCodeImage, entries []int) (*NativeTailOwner, error) {
				if sparse {
					return NewNativeTailSparseImportedOwner(image, storage.job, o.context, o.descriptors, o.directory, entries, []*wasm.CompType{&sig}, []NativeTailImport{importRow}, trampoline)
				}
				return NewNativeTailImportedOwner(image, storage.job, o.context, o.descriptors, o.directory, entries, []wasm.CompType{sig}, []NativeTailImport{importRow}, trampoline)
			}
			owner, err := build(nil, nil)
			if err != nil {
				t.Fatal("certified import-only owner", err)
			}
			am, ac, aerr := NativeTailDefinedFactoryBudget(1, 4, 1, []*NativeTailOwner{owner}, codeBytes)
			cm, cc, cerr := NativeTailFactoryCountsBudget(1, 4, 1, counts, codeBytes)
			if aerr != nil || cerr != nil || am != cm || ac != cc {
				t.Fatal("import-only count/frozen budget", am, cm, ac, cc, aerr, cerr)
			}
			for _, invalid := range []struct {
				image   *NativeTailCodeImage
				entries []int
			}{{wrapper.published, nil}, {nil, []int{0}}} {
				if got, err := build(invalid.image, invalid.entries); got != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("import-only owner accepted fabricated guest span", err)
				}
			}
			if n := testing.AllocsPerRun(4, func() {
				if err := owner.Validate(); err != nil {
					panic(err)
				}
			}); n != 0 {
				t.Fatal("warm import-only validation allocated", n)
			}
			if _, err := NewNativeTailFunction(wrapper.published, storage.job, o.context, o.descriptors, 0, len(wrapper.snapshot), 0, sig); !errors.Is(err, ErrUnsupported) {
				t.Fatal("import wrapper accepted as genuine guest frame", err)
			}
			lease.err = errors.New("retired import-only owner")
			if err := owner.Validate(); !errors.Is(err, lease.err) {
				t.Fatal("retired wrapper image lost owner", err)
			}
			if got, err := build(nil, nil); got != nil || !errors.Is(err, lease.err) {
				t.Fatal("retired wrapper published new owner", err)
			}
		})
	}
}
