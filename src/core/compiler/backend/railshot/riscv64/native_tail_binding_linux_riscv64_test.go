//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestTailTypedFactoryRebindingExactDeclaredKey(t *testing.T) {
	sig := wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.F32}}
	f := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
	o := tailDynamicOwnerForTest(t, []tailRAFunction{f})
	f, _ = tailPublishedFunctionForTest(t, o.functions[0])
	key := wasm.StructuralFuncTypeKey(&sig) ^ 0x9e3779b97f4a7c15
	binary.LittleEndian.PutUint64(o.descriptors[48:], key)
	provider := &tailFunctionKeyOwnerForTest{NativeTailCodeOwner: f.published.owner, key: key, job: f.job, context: f.context}
	image, err := NewNativeTailCodeImage(provider)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewNativeTailDefinedOwner(image, f.job, f.context, o.descriptors, o.directory, []int{0}, []wasm.CompType{sig})
	if err != nil {
		t.Fatal(err)
	}
	site := tailDynamicSiteForTest(t, f, nil, f.info.FrameBytes-8)
	site.dynamic = nil
	site, err = NativeTailDefinedTypedSite(site, f.info.FrameBytes-8, []*NativeTailDefinedOwner{owner}, image, 0)
	if err != nil {
		t.Fatal(err)
	}
	factory := tailDynamicBindForTest(t, site, len(f.snapshot))
	if err := factory.Detach(); err != nil {
		t.Fatal(err)
	}
	binding := f.context[abi.RV64TailCapabilityOffset:]
	provider.key ^= 8
	if err := factory.Bind(owner); !errors.Is(err, ErrUnsupported) || binary.LittleEndian.Uint64(binding) != 0 {
		t.Fatal("changed declared key rebound", err)
	}
	provider.key = key
	provider.err = errors.New("declared metadata retired")
	if err := factory.Bind(owner); !errors.Is(err, provider.err) || binary.LittleEndian.Uint64(binding) != 0 {
		t.Fatal("unavailable declared metadata rebound", err)
	}
	provider.err = nil
	if n := testing.AllocsPerRun(20, func() {
		if err := factory.Bind(owner); err != nil {
			panic(err)
		}
		if err := factory.Detach(); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm typed rebinding allocated", n)
	}
}

func TestTailInvocationFactoryRebindingOwnership(t *testing.T) {
	sig := wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.F64}, Results: []wasm.ValType{wasm.V128}}
	makeOwner := func() (tailRAFunction, *NativeTailDefinedOwner, *tailPublishedOwnerForTest) {
		f := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
		o := tailDynamicOwnerForTest(t, []tailRAFunction{f})
		f, lease := tailPublishedFunctionForTest(t, o.functions[0])
		owner, err := NewNativeTailDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, []int{0}, []wasm.CompType{sig})
		if err != nil {
			t.Fatal(err)
		}
		return f, owner, lease
	}
	caller, callerOwner, callerLease := makeOwner()
	target, targetOwner, targetLease := makeOwner()
	s := tailDynamicSiteForTest(t, caller, nil, caller.info.FrameBytes-8)
	s.dynamic = nil
	// The dynamic target set deliberately excludes the caller. Rebinding must
	// authenticate its physical context through the explicit caller argument.
	s, err := NativeTailDefinedSite(s, caller.info.FrameBytes-8, []*NativeTailDefinedOwner{targetOwner})
	if err != nil {
		t.Fatal(err)
	}
	first := tailDynamicBindForTest(t, s, len(caller.snapshot)+len(target.snapshot))
	firstBase, firstCap := first.code.Base(), tailRAPointer(first.capability)
	if err := first.Detach(); err != nil || first.code.Base() != firstBase || tailRAPointer(first.capability) != firstCap || !first.ready {
		t.Fatal("detach changed published storage", err)
	}
	second := tailDynamicBindForTest(t, s, len(caller.snapshot)+len(target.snapshot))
	secondCap := uint64(tailRAPointer(second.capability))
	binding := caller.context[abi.RV64TailCapabilityOffset:]
	if err := first.Bind(callerOwner); !errors.Is(err, ErrUnsupported) || binary.LittleEndian.Uint64(binding) != secondCap {
		t.Fatal("outer factory replaced nested binding", err)
	}
	if err := first.Close(); err != nil || binary.LittleEndian.Uint64(binding) != secondCap {
		t.Fatal("detached close changed nested binding", err)
	}
	if err := first.Bind(callerOwner); !errors.Is(err, ErrUnsupported) {
		t.Fatal("closed factory rebound", err)
	}
	if err := second.Detach(); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []*NativeTailDefinedOwner{nil, targetOwner} {
		if err := second.Bind(owner); !errors.Is(err, ErrUnsupported) || binary.LittleEndian.Uint64(binding) != 0 {
			t.Fatal("wrong caller bound", err)
		}
	}
	expired := errors.New("expired particular physical lease")
	callerLease.err = expired
	if err := second.Bind(callerOwner); !errors.Is(err, expired) || binary.LittleEndian.Uint64(binding) != 0 {
		t.Fatal("expired caller rebound", err)
	}
	callerLease.err = nil
	targetLease.err = expired
	if err := second.Bind(callerOwner); !errors.Is(err, expired) || binary.LittleEndian.Uint64(binding) != 0 {
		t.Fatal("expired target rebound", err)
	}
	targetLease.err = nil
	row := targetOwner.owner.descriptors[48:56]
	key := binary.LittleEndian.Uint64(row)
	binary.LittleEndian.PutUint64(row, key^8)
	if err := second.Bind(callerOwner); !errors.Is(err, ErrUnsupported) || binary.LittleEndian.Uint64(binding) != 0 {
		t.Fatal("changed live target rebound", err)
	}
	binary.LittleEndian.PutUint64(row, key)
	if err := second.Bind(callerOwner); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(4, func() {
		if err := second.Detach(); err != nil {
			panic(err)
		}
		if err := second.Bind(callerOwner); err != nil {
			panic(err)
		}
		if err := second.Bind(callerOwner); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm binding allocated", n)
	}
	// Failed target validation must not obstruct detaching a still-live caller.
	targetLease.err = expired
	if err := second.Detach(); err != nil || binary.LittleEndian.Uint64(binding) != 0 {
		t.Fatal("target retirement blocked detach", err)
	}
	targetLease.err = nil
	if err := second.Bind(callerOwner); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(binding, 0)
	if err := second.Detach(); err != nil {
		t.Fatal("already cleared binding", err)
	}
	if err := second.Bind(callerOwner); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(binding, 8)
	if err := second.Detach(); !errors.Is(err, ErrUnsupported) || binary.LittleEndian.Uint64(binding) != 8 {
		t.Fatal("detach overwrote foreign binding", err)
	}
	if err := second.Close(); !errors.Is(err, ErrUnsupported) || second.ready || second.code.Base() == 0 || binary.LittleEndian.Uint64(binding) != 8 {
		t.Fatal("failed close lost cleanup ownership or callable revocation", err)
	}
	if err := second.Bind(callerOwner); !errors.Is(err, ErrUnsupported) {
		t.Fatal("failed close rebound", err)
	}
	binary.LittleEndian.PutUint64(binding, secondCap)
	if err := second.Close(); err != nil || binary.LittleEndian.Uint64(binding) != 0 {
		t.Fatal("retained close retry", err)
	}
}
