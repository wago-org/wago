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

type tailFunctionKeyOwnerForTest struct {
	NativeTailCodeOwner
	key     uint64
	err     error
	job     *coreruntime.JobMemory
	context []byte
}

func (o *tailFunctionKeyOwnerForTest) NativeTailTypeKey(index uint32, params, results []wasm.ValType) (uint64, error) {
	return o.NativeTailFunctionKey(index, params, results)
}

func (o *tailFunctionKeyOwnerForTest) NativeTailContextView() (*coreruntime.JobMemory, []byte, error) {
	return o.job, o.context, nil
}

func (o *tailFunctionKeyOwnerForTest) NativeTailFunctionKey(index uint32, params, results []wasm.ValType) (uint64, error) {
	if o.err != nil {
		return 0, o.err
	}
	if index != 0 || len(params) != 1 || !wasm.EqualValType(params[0], wasm.I32) || len(results) != 1 || !wasm.EqualValType(results[0], wasm.F32) {
		return 0, unsupported("test retained function signature")
	}
	return o.key, nil
}

func TestNativeTailDefinedOwnerExactPublishedKey(t *testing.T) {
	sig := wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.F32}}
	f := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
	o := tailDynamicOwnerForTest(t, []tailRAFunction{f})
	f, lease := tailPublishedFunctionForTest(t, o.functions[0])
	key := wasm.StructuralFuncTypeKey(&sig) ^ 0x9e3779b97f4a7c15
	binary.LittleEndian.PutUint64(o.descriptors[48:], key)
	provider := &tailFunctionKeyOwnerForTest{NativeTailCodeOwner: f.published.owner, key: key, job: f.job, context: o.context}
	image, err := NewNativeTailCodeImage(provider)
	if err != nil {
		t.Fatal(err)
	}
	build := func(signature wasm.CompType) (*NativeTailDefinedOwner, error) {
		return NewNativeTailDefinedOwner(image, f.job, o.context, o.descriptors, o.directory, []int{0}, []wasm.CompType{signature})
	}
	owner, err := build(sig)
	if err != nil || owner.owner.signatureKeys[0] != key {
		t.Fatal("exact retained key", err)
	}
	site := tailDynamicSiteForTest(t, f, nil, f.info.FrameBytes-8)
	site.dynamic = nil
	typed, err := NativeTailDefinedTypedSite(site, f.info.FrameBytes-8, []*NativeTailDefinedOwner{owner}, image, 0)
	if err != nil || typed.dynamic.key != key {
		t.Fatal("exact declared site key", err)
	}
	proof := tailRAProofSite{dynamic: typed.dynamic, invocationContext: true, frameBytes: site.FrameBytes, callerHome: site.CallerHome, callerContext: tailRAPointer(site.CallerContext), params: site.Parameters, results: site.Results, sourceBase: site.SourceBase, sourceStride: site.SourceStride}
	if n := testing.AllocsPerRun(20, func() {
		if err := typed.dynamic.validate(&proof); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm typed site validation allocated", n)
	}
	if _, err := NativeTailDefinedTypedSite(site, f.info.FrameBytes-8, []*NativeTailDefinedOwner{owner}, image, 1); !errors.Is(err, ErrUnsupported) {
		t.Fatal("wrong declared type index accepted", err)
	}
	provider.context = append([]byte(nil), o.context...)
	if err := typed.dynamic.validate(&proof); !errors.Is(err, ErrUnsupported) {
		t.Fatal("foreign caller context accepted", err)
	}
	provider.context = o.context
	if n := testing.AllocsPerRun(20, func() {
		if err := owner.Validate(); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm owner validation allocated", n)
	}
	wrong := wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: sig.Results}
	if _, err := build(wrong); !errors.Is(err, ErrUnsupported) {
		t.Fatal("wrong transport accepted", err)
	}
	provider.err = errors.New("key metadata unavailable")
	if _, err := build(sig); !errors.Is(err, provider.err) {
		t.Fatal("failed metadata lookup accepted", err)
	}
	provider.err = nil
	binary.LittleEndian.PutUint64(o.descriptors[48:], key^8)
	if err := owner.Validate(); !errors.Is(err, ErrUnsupported) {
		t.Fatal("changed exact descriptor key accepted", err)
	}
	binary.LittleEndian.PutUint64(o.descriptors[48:], key)
	lease.err = errors.New("physical owner retired")
	if err := owner.Validate(); !errors.Is(err, lease.err) {
		t.Fatal("retired exact-key owner accepted", err)
	}
}
