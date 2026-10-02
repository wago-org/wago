package compiler

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
)

func TestRouterSelectsOneEngineAndStampsOutput(t *testing.T) {
	m := &wasm.Module{}
	calls := [2]int{}
	router := Router{
		Railshot: BackendFunc(func(Input) (Output, error) { calls[0]++; return Output{}, nil }),
		Dragline: BackendFunc(func(Input) (Output, error) { calls[1]++; return Output{}, nil }),
	}
	output, err := router.Compile(EngineDragline, Input{Module: m, Runtime: RuntimeContract{ABIRevision: runtimeabi.Revision}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != [2]int{0, 1} {
		t.Fatalf("backend calls = %v, want only Dragline", calls)
	}
	if output.Engine != EngineDragline {
		t.Fatalf("output engine = %v, want dragline", output.Engine)
	}
}

func TestRouterRejectsInvalidBoundaryInputs(t *testing.T) {
	backendErr := errors.New("strict failure")
	router := Router{Dragline: BackendFunc(func(Input) (Output, error) { return Output{}, backendErr })}
	valid := Input{Module: &wasm.Module{}, Runtime: RuntimeContract{ABIRevision: runtimeabi.Revision}}

	if _, err := router.Compile(Engine(99), valid); err == nil {
		t.Fatal("unknown engine accepted")
	}
	if _, err := router.Compile(EngineDragline, Input{Runtime: valid.Runtime}); err == nil {
		t.Fatal("nil validated module accepted")
	}
	wrongABI := valid
	wrongABI.Runtime.ABIRevision++
	if _, err := router.Compile(EngineDragline, wrongABI); err == nil {
		t.Fatal("wrong runtime ABI accepted")
	}
	if _, err := router.Compile(EngineDragline, valid); !errors.Is(err, backendErr) {
		t.Fatalf("backend error = %v, want wrapped sentinel", err)
	}
}
