//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestNativeImportCertificatePublishedIdentity(t *testing.T) {
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	source := tailRAProofFunctionFixture(t, sig, abi.RV64InstanceContextBytes)
	target := tailRAProofFunctionFixture(t, sig, abi.RV64InstanceContextBytes)
	_, proof := tailDynamicProxyForTest(t, source, target, 0)
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		t.Fatal(err)
	}
	callee := NativeImportTarget{Entry: target.code.Base(), Home: target.job.LinMemBase(), Context: tailRAPointer(target.context), Arguments: proof.arguments, Results: proof.results, ArgumentSlots: 1, ResultSlots: 1, StackOwnedActivation: true, InvocationContext: true, HostTrampoline: trampoline}
	home, context := source.job.LinMemBase(), tailRAPointer(source.context)
	code, certificate, err := NativeImportThunkWithCertificate(callee, home, context, 0)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := NativeImportThunk(callee, home, context, 0)
	if err != nil || !bytes.Equal(plain, code) {
		t.Fatal("certificate changed bridge bytes", err)
	}
	buffer := tailRAProofBuffer(t, code)
	if err := buffer.Seal(); err != nil {
		t.Fatal(err)
	}
	lease := &tailPublishedOwnerForTest{view: buffer.Bytes()}
	image, err := NewNativeTailCodeImage(lease)
	if err != nil {
		t.Fatal(err)
	}
	if err := certificate.Validate(image, 0, callee, home, context, 0); err != nil {
		t.Fatal(err)
	}
	proxy := tailRAFunction{published: image, snapshot: image.Bytes(), signature: sig, info: FunctionInfo{Entry: 0, CodeBytes: len(code)}, job: source.job, context: source.context, directory: source.directory}
	proof.certificate = certificate
	if err := proof.validate(proxy, target, trampoline); err != nil {
		t.Fatal("certified proxy", err)
	}
	if n := testing.AllocsPerRun(4, func() {
		if err := proof.validate(proxy, target, trampoline); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm proxy regenerated its bridge", n)
	}
	callee.Arguments[0] = 42
	callee.Results[0] = 99
	if n := testing.AllocsPerRun(4, func() {
		if err := certificate.Validate(image, 0, callee, home, context, 0); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm certificate validation allocated", n)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*NativeImportTarget)
	}{
		{"entry", func(t *NativeImportTarget) { t.Entry += 16 }}, {"home", func(t *NativeImportTarget) { t.Home += 8 }}, {"context", func(t *NativeImportTarget) { t.Context += 8 }},
		{"args", func(t *NativeImportTarget) { t.Arguments = t.Results }}, {"length", func(t *NativeImportTarget) { t.Results = t.Results[:8] }}, {"slots", func(t *NativeImportTarget) { t.ArgumentSlots++ }},
		{"stack", func(t *NativeImportTarget) { t.StackOwnedActivation = false }}, {"view", func(t *NativeImportTarget) { t.InvocationContext = false }}, {"trampoline", func(t *NativeImportTarget) { t.HostTrampoline += 4 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := callee
			tc.mutate(&changed)
			if err := certificate.Validate(image, 0, changed, home, context, 0); !errors.Is(err, ErrUnsupported) {
				t.Fatal("changed bridge identity accepted", err)
			}
		})
	}
	for _, off := range []int{-16, 4, 16, len(code)} {
		if err := certificate.Validate(image, off, callee, home, context, 0); !errors.Is(err, ErrUnsupported) {
			t.Fatal("wrong logical bridge span", off, err)
		}
	}
	if err := certificate.Validate(image, 0, callee, home, context, 1); !errors.Is(err, ErrUnsupported) {
		t.Fatal("wrong import index", err)
	}
	changed := bytes.Clone(code)
	changed[0] ^= 4
	other := tailRAProofBuffer(t, changed)
	if err := other.Seal(); err != nil {
		t.Fatal(err)
	}
	bad, err := NewNativeTailCodeImage(&tailPublishedOwnerForTest{view: other.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	if err := certificate.Validate(bad, 0, callee, home, context, 0); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unissued wrapper accepted", err)
	}
	lease.err = errors.New("retired importer")
	if err := certificate.Validate(image, 0, callee, home, context, 0); !errors.Is(err, lease.err) {
		t.Fatal("retired image read", err)
	}
	callee.Entry = 0
	if code, c, err := NativeImportThunkWithCertificate(callee, home, context, 0); code != nil || c != nil || err == nil {
		t.Fatal("invalid builder issued certificate", err)
	}
}
