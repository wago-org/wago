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

type tailContextCodeOwnerForTest struct {
	NativeTailCodeOwner
	job     *coreruntime.JobMemory
	context []byte
}

func (o *tailContextCodeOwnerForTest) NativeTailContextView() (*coreruntime.JobMemory, []byte, error) {
	return o.job, o.context, nil
}

func TestNativeTailContextOnlyProducerIdentity(t *testing.T) {
	sig := wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	f := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
	f, lease := tailPublishedFunctionForTest(t, f)
	binary.LittleEndian.PutUint64(f.context[16:], 0)
	owner := &tailContextCodeOwnerForTest{f.published.owner, f.job, f.context}
	image, err := NewNativeTailCodeImage(owner)
	if err != nil {
		t.Fatal(err)
	}
	build := func(job *coreruntime.JobMemory, context []byte) (NativeTailFunction, error) {
		return NewNativeTailFunction(image, job, context, nil, f.info.Entry, f.info.Entry+f.info.CodeBytes, 0, sig)
	}
	target, err := build(f.job, f.context)
	if err != nil || len(target.function.directory) != 0 {
		t.Fatal("context-only producer", err)
	}
	if allocations := testing.AllocsPerRun(20, func() {
		if err := target.function.validate(true); err != nil {
			panic(err)
		}
	}); allocations != 0 {
		t.Fatal("context authentication allocated", allocations)
	}
	other := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
	if _, err := build(other.job, f.context); !errors.Is(err, ErrUnsupported) {
		t.Fatal("foreign job accepted", err)
	}
	if _, err := build(f.job, append([]byte(nil), f.context...)); !errors.Is(err, ErrUnsupported) {
		t.Fatal("copied context accepted", err)
	}
	if _, err := NewNativeTailFunction(f.published, f.job, f.context, nil, f.info.Entry, f.info.Entry+f.info.CodeBytes, 0, sig); !errors.Is(err, ErrUnsupported) {
		t.Fatal("context without published owner proof accepted", err)
	}
	binary.LittleEndian.PutUint64(f.context[16:], 8)
	if err := target.function.validate(true); !errors.Is(err, ErrUnsupported) {
		t.Fatal("changed context anchor accepted", err)
	}
	binary.LittleEndian.PutUint64(f.context[16:], 0)
	lease.err = errors.New("producer physically retired")
	if err := target.function.validate(true); !errors.Is(err, lease.err) {
		t.Fatal("retired context-only producer accepted", err)
	}
}
