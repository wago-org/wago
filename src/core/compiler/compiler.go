// Package compiler defines the stable boundary between Wago's compiler engines
// and the runtime-facing compilation pipeline.
package compiler

import (
	"fmt"

	"github.com/wago-org/wago/src/core/codeimage"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
)

// Engine identifies an independent compiler pipeline.
type Engine uint8

const (
	EngineRailshot Engine = iota
	EngineDragline
)

func (e Engine) String() string {
	switch e {
	case EngineRailshot:
		return "railshot"
	case EngineDragline:
		return "dragline"
	default:
		return fmt.Sprintf("CompilerEngine(%d)", uint8(e))
	}
}

// Valid reports whether e names a compiler engine understood by this build.
func (e Engine) Valid() bool { return e == EngineRailshot || e == EngineDragline }

// RuntimeContract contains stable runtime facts visible to every compiler
// engine. New fields belong here only when they are engine-neutral ABI facts.
type RuntimeContract struct {
	ABIRevision uint32
}

// Target identifies the machine-code product requested from an engine.
type Target struct {
	GOOS   string
	GOARCH string
}

// Input is the immutable, validated input shared by sibling compiler engines.
// Module has passed Wasm validation before Compile is called. Implementations
// must treat it as read-only and must not retain mutable views into it.
type Input struct {
	Module  *wasm.Module
	Runtime RuntimeContract
	Target  Target
}

// Output is the runtime-facing native-code product shared by compiler engines.
// Compiler-private IR, ABI decisions, and optimization state must not escape in
// this structure.
type Output struct {
	Engine Engine

	CodeImage codeimage.Image
	Code      []byte

	Entry          []int
	InternalEntry  []int
	DirectPrepared []uint64

	RequiresBMI2   bool
	RequiresAVX2   bool
	RequiresAVX512 bool
}

// Backend is one complete, independent compiler engine.
type Backend interface {
	Compile(Input) (Output, error)
}

// BackendFunc adapts a function to Backend.
type BackendFunc func(Input) (Output, error)

func (f BackendFunc) Compile(input Input) (Output, error) { return f(input) }

// Router selects exactly one sibling engine. It never delegates between them.
type Router struct {
	Railshot Backend
	Dragline Backend
}

// Compile invokes only engine and stamps its identity on the returned output.
func (r Router) Compile(engine Engine, input Input) (Output, error) {
	if !engine.Valid() {
		return Output{}, fmt.Errorf("compiler: unknown engine %d", uint8(engine))
	}
	if input.Module == nil {
		return Output{}, fmt.Errorf("compiler %s: nil validated module", engine)
	}
	if input.Runtime.ABIRevision != runtimeabi.Revision {
		return Output{}, fmt.Errorf("compiler %s: runtime ABI revision %d unsupported (want %d)", engine, input.Runtime.ABIRevision, runtimeabi.Revision)
	}
	var backend Backend
	switch engine {
	case EngineRailshot:
		backend = r.Railshot
	case EngineDragline:
		backend = r.Dragline
	}
	if backend == nil {
		return Output{}, fmt.Errorf("compiler %s: backend is not installed", engine)
	}
	output, err := backend.Compile(input)
	if err != nil {
		return Output{}, fmt.Errorf("compiler %s: %w", engine, err)
	}
	output.Engine = engine
	return output, nil
}
