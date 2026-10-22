//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// NativeTailCodeOwner supplies an exact logical image from a trusted runtime
// code owner, already RX and synchronized for process-wide instruction fetch.
// The caller must hold that owner's physical execution lease through native
// execution, parks, resume and cold decoration. A Go pointer or another
// instance's reference to the same cache is not such a lease.
type NativeTailCodeOwner interface {
	NativeTailCodeView() ([]byte, error)
}

// NativeTailContextOwner authenticates a definition that needs no descriptor
// header. The same retained runtime owner supplies its exact job and context.
// This optional native lease contract is never serialized.
type NativeTailContextOwner interface {
	NativeTailContextView() (*coreruntime.JobMemory, []byte, error)
}

// NativeTailFunctionKeyOwner supplies the exact descriptor key for a function
// of the retained image. It must check that the supplied transport signature
// matches that function's immutable metadata. The key is not an ABI-type hash:
// indexed and recursive types can share a transport signature.
type NativeTailFunctionKeyOwner interface {
	NativeTailFunctionKey(uint32, []wasm.ValType, []wasm.ValType) (uint64, error)
}

// NativeTailTypeKeyOwner checks an exact declared type against its transport
// signature under the retained runtime image lease.
type NativeTailTypeKeyOwner interface {
	NativeTailTypeKey(uint32, []wasm.ValType, []wasm.ValType) (uint64, error)
}

// NativeTailTypeMatchOwner compares immutable declared graphs while building
// a dispatcher. The runtime must authenticate both particular image leases.
// Emitted dispatch retains the target's exact key; no subtype search runs in
// guest code or adds a per-site retained match array.
type NativeTailTypeMatchOwner interface {
	NativeTailTypeMatchesFunction(uint32, NativeTailCodeOwner, uint32) (bool, error)
}

func (i *NativeTailCodeImage) typeMatchesFunction(index uint32, target *NativeTailCodeImage, function uint32) (bool, error) {
	if err := i.Validate(); err != nil {
		return false, err
	}
	if target == nil {
		return false, unsupported("native typed tail target has no retained image")
	}
	if err := target.Validate(); err != nil {
		return false, err
	}
	owner, ok := i.owner.(NativeTailTypeMatchOwner)
	if !ok {
		return false, nil
	}
	return owner.NativeTailTypeMatchesFunction(index, target.owner, function)
}

func (i *NativeTailCodeImage) typeKey(index uint32, params, results []wasm.ValType) (uint64, error) {
	if err := i.Validate(); err != nil {
		return 0, err
	}
	owner, ok := i.owner.(NativeTailTypeKeyOwner)
	if !ok {
		return 0, unsupported("native tail declared type requires its published runtime owner")
	}
	return owner.NativeTailTypeKey(index, params, results)
}

func (i *NativeTailCodeImage) validateCaller(home, context uintptr) error {
	if err := i.Validate(); err != nil {
		return err
	}
	owner, ok := i.owner.(NativeTailContextOwner)
	if !ok {
		return unsupported("native typed tail caller requires its published context owner")
	}
	job, actual, err := owner.NativeTailContextView()
	if err != nil {
		return err
	}
	if job == nil || job.LinMemBase() != home || tailRAPointer(actual) != context {
		return unsupported("native typed tail caller image/context identity")
	}
	return nil
}

func (i *NativeTailCodeImage) functionKey(index uint32, signature *wasm.CompType) (uint64, error) {
	if i == nil {
		return wasm.StructuralFuncTypeKey(signature), nil
	}
	if err := i.Validate(); err != nil {
		return 0, err
	}
	if owner, ok := i.owner.(NativeTailFunctionKeyOwner); ok {
		return owner.NativeTailFunctionKey(index, signature.Params, signature.Results)
	}
	return wasm.StructuralFuncTypeKey(signature), nil
}

func (i *NativeTailCodeImage) validateContext(job *coreruntime.JobMemory, context []byte) error {
	if err := i.Validate(); err != nil {
		return err
	}
	owner, ok := i.owner.(NativeTailContextOwner)
	if !ok {
		return unsupported("native tail context requires its published runtime owner")
	}
	actualJob, actualContext, err := owner.NativeTailContextView()
	if err != nil {
		return err
	}
	if job != actualJob || len(context) != len(actualContext) || tailRAPointer(context) != tailRAPointer(actualContext) {
		return unsupported("native tail published context identity")
	}
	return nil
}

// NativeTailCodeImage borrows an immutable published image. It retains its Go
// owner, but neither acquires a physical lease nor seals or unmaps the image.
// It has no serialized representation and accepts no raw code address.
type NativeTailCodeImage struct {
	owner NativeTailCodeOwner
	bytes []byte
	base  uintptr
}

func NewNativeTailCodeImage(owner NativeTailCodeOwner) (*NativeTailCodeImage, error) {
	if owner == nil {
		return nil, unsupported("native tail published code owner")
	}
	b, err := owner.NativeTailCodeView()
	if err != nil {
		return nil, err
	}
	base := tailRAPointer(b)
	if len(b) == 0 || len(b) > MaxCodeBytes || len(b)&3 != 0 || base == 0 || base&15 != 0 || uintptr(len(b)) > ^uintptr(0)-base {
		return nil, unsupported("native tail published code span")
	}
	return &NativeTailCodeImage{owner: owner, bytes: b[:len(b):len(b)], base: base}, nil
}

// Validate must also run after a park, under the reacquired owning lease,
// before an existing image participates in a refreshed target factory.
func (i *NativeTailCodeImage) Validate() error {
	if i == nil || i.owner == nil {
		return unsupported("native tail missing published image")
	}
	b, err := i.owner.NativeTailCodeView()
	if err != nil {
		return err
	}
	if len(b) != len(i.bytes) || tailRAPointer(b) != i.base {
		return unsupported("native tail published image replaced")
	}
	return nil
}

// Base and Bytes describe borrowed RX storage, usable only under the physical
// lease and after Validate. Bytes must never be written by a consumer.
func (i *NativeTailCodeImage) Base() uintptr {
	if i == nil {
		return 0
	}
	return i.base
}
func (i *NativeTailCodeImage) Bytes() []byte {
	if i == nil {
		return nil
	}
	return i.bytes
}

type tailRACodeKey struct {
	buffer *coreruntime.CodeBuffer
	base   uintptr
	bytes  int
}

func (f tailRAFunction) codeKey() tailRACodeKey {
	if f.published != nil {
		return tailRACodeKey{base: f.published.Base(), bytes: len(f.published.Bytes())}
	}
	return tailRACodeKey{buffer: f.code}
}
func (f tailRAFunction) codeBase() uintptr {
	if f.published != nil {
		return f.published.Base()
	}
	return f.code.Base()
}
func (f tailRAFunction) codeBytes() []byte {
	if f.published != nil {
		return f.published.Bytes()
	}
	return f.code.Bytes()
}
func (f tailRAFunction) validateCode() error {
	if (f.code == nil) == (f.published == nil) {
		return unsupported("native tail exact code owner")
	}
	if f.published != nil {
		return f.published.Validate()
	}
	return nil
}
func (f tailRAFunction) publishCode() error {
	if err := f.validateCode(); err != nil {
		return err
	}
	if f.published != nil {
		return nil // Publication belongs to the borrowed runtime owner.
	}
	return f.code.Seal()
}
