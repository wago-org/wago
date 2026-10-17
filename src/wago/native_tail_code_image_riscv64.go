//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// This borrowed adapter is used only while the invocation's retained owner
// graph holds the particular instance's physical resources. It acquires no
// roots, changes no mapping protections and owns no cleanup. The graph must
// refresh/revalidate it after each park before executing a target again.
type rv64TailCodeOwner struct{ instance *Instance }

func (o rv64TailCodeOwner) NativeTailCodeView() ([]byte, error) {
	return o.nativeOwnerCodeView(false)
}

func (o rv64TailCodeOwner) NativeTailFunctionKey(index uint32, params, results []wasm.ValType) (uint64, error) {
	return o.nativeTailFunctionKey(index, params, results, false)
}

func (o rv64TailCodeOwner) nativeTailFunctionKey(index uint32, params, results []wasm.ValType, allowEmpty bool) (uint64, error) {
	if _, err := o.nativeOwnerCodeView(allowEmpty); err != nil {
		return 0, err
	}
	c := o.instance.c
	if uint64(index) >= uint64(len(c.FuncTypeID)) {
		return 0, fmt.Errorf("%w: RV64 tail function key index", railshot.ErrUnsupported)
	}
	var sig FuncSig
	if int(index) < c.NumImports {
		if int(index) >= len(c.importFuncSigs) {
			return 0, fmt.Errorf("%w: RV64 tail import key index", railshot.ErrUnsupported)
		}
		sig = c.importFuncSigs[index]
	} else {
		local := int(index) - c.NumImports
		if local >= len(c.Funcs) {
			return 0, fmt.Errorf("%w: RV64 tail definition key index", railshot.ErrUnsupported)
		}
		sig = c.Funcs[local]
	}
	if !rv64TailTransportEqual(sig.Params, params) || !rv64TailTransportEqual(sig.Results, results) {
		return 0, fmt.Errorf("%w: RV64 tail key transport signature", railshot.ErrUnsupported)
	}
	return rv64AttachedFunctionTypeKey(o.instance, int(index)), nil
}

func (o rv64TailCodeOwner) NativeTailTypeKey(index uint32, params, results []wasm.ValType) (uint64, error) {
	if _, err := o.nativeOwnerCodeView(false); err != nil {
		return 0, err
	}
	c := o.instance.c
	indexes := c.loadCompileIndexes()
	if uint64(index) >= uint64(len(c.Types)) || !(indexes != nil && uint64(index) < uint64(len(indexes.railshotTail.primitiveTailTypeKeys)) && (rv64FreshRootedGC(c) || indexes.railshotTail.primitiveReferences)) {
		return 0, fmt.Errorf("%w: RV64 tail declared type owner", railshot.ErrUnsupported)
	}
	typ := &c.Types[index]
	if typ.Kind != CompositeTypeFunction || !rv64TailDeclaredTransportEqual(c.Types, typ.Params, params) || !rv64TailDeclaredTransportEqual(c.Types, typ.Results, results) {
		return 0, fmt.Errorf("%w: RV64 tail declared transport signature", railshot.ErrUnsupported)
	}

	return indexes.railshotTail.primitiveTailTypeKeys[index], nil
}

type rv64TailFunctionTypeOwner interface {
	nativeTailFunctionType(uint32) (*Compiled, uint32, error)
}

func rv64TailDeclaredFunctionType(c *Compiled, index uint32) (*Compiled, uint32, error) {
	if c == nil || uint64(index) >= uint64(len(c.FuncTypeID)) {
		return nil, 0, rv64TailGenerationError("declared target function index")
	}
	var sig FuncSig
	if int(index) < c.NumImports {
		if int(index) >= len(c.importFuncSigs) {
			return nil, 0, rv64TailGenerationError("declared import function index")
		}
		sig = c.importFuncSigs[index]
	} else {
		local := int(index) - c.NumImports
		if local >= len(c.Funcs) {
			return nil, 0, rv64TailGenerationError("declared local function index")
		}
		sig = c.Funcs[local]
	}
	if !sig.HasTypeIndex || uint64(sig.TypeIndex) >= uint64(len(c.Types)) || c.Types[sig.TypeIndex].Kind != CompositeTypeFunction {
		return nil, 0, rv64TailGenerationError("declared target function type")
	}
	return c, sig.TypeIndex, nil
}

func (o rv64TailCodeOwner) nativeTailFunctionType(index uint32) (*Compiled, uint32, error) {
	if _, err := o.NativeTailCodeView(); err != nil {
		return nil, 0, err
	}
	if int(index) < o.instance.c.NumImports {
		entries := o.instance.importReferenceEntries()
		if int(index) >= len(entries) {
			return nil, 0, rv64TailGenerationError("dynamic import declared owner")
		}
		if target := entries[index].native; target != nil {
			if target.inst == nil || target.inst.c == nil || target.localIdx < 0 || target.localIdx >= len(target.inst.c.Funcs) {
				return nil, 0, rv64TailGenerationError("dynamic import producer type")
			}
			return rv64TailDeclaredFunctionType(target.inst.c, uint32(target.inst.c.NumImports+target.localIdx))
		}
	}
	return rv64TailDeclaredFunctionType(o.instance.c, index)
}

func (o rv64TailCodeOwner) NativeTailTypeMatchesFunction(index uint32, target railshot.NativeTailCodeOwner, function uint32) (bool, error) {
	if _, err := o.NativeTailCodeView(); err != nil {
		return false, err
	}
	c := o.instance.c
	indexes := c.loadCompileIndexes()
	if uint64(index) >= uint64(len(c.Types)) || c.Types[index].Kind != CompositeTypeFunction || !(indexes != nil && uint64(index) < uint64(len(indexes.railshotTail.primitiveTailTypeKeys)) && (rv64FreshRootedGC(c) || indexes.railshotTail.primitiveReferences)) {
		return false, rv64TailGenerationError("subtype caller declared type")
	}
	owner, ok := target.(rv64TailFunctionTypeOwner)
	if !ok {
		return false, rv64TailGenerationError("subtype target declared owner")
	}
	actual, actualIndex, err := owner.nativeTailFunctionType(function)
	if err != nil {
		return false, err
	}
	if actual == c && actualIndex == index {
		return true, nil // Identical retained declaration after owner authentication.
	}
	actualRef := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: actualIndex}}
	requiredRef := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: index}}
	return referenceTypeSubtype(actualRef, actual.Types, requiredRef, c.Types), nil
}

func rv64TailDeclaredTransportEqual(types []DefinedTypeDescriptor, expected []ValueTypeDescriptor, actual []wasm.ValType) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i, value := range expected {
		typ, ok := value.ABIType(types)
		if !ok || !rv64TailTransportEqual([]ValType{typ}, actual[i:i+1]) {
			return false
		}
	}
	return true
}

func rv64TailTransportEqual(expected []ValType, actual []wasm.ValType) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i, v := range expected {
		var want wasm.ValType
		switch v {
		case ValI32:
			want = wasm.I32
		case ValI64:
			want = wasm.I64
		case ValF32:
			want = wasm.F32
		case ValF64:
			want = wasm.F64
		case ValV128:
			want = wasm.V128
		case ValFuncRef:
			want = wasm.FuncRef
		case ValExternRef:
			want = wasm.ExternRef
		case ValAnyRef:
			want = wasm.AnyRef
		case ValI31Ref:
			want = wasm.I31Ref
		case ValExnRef:
			want = wasm.RefVal(wasm.AbsRef(wasm.HeapExn))
		default:
			return false
		}
		if !wasm.EqualValType(want, actual[i]) {
			return false
		}
	}
	return true
}

func (o rv64TailCodeOwner) NativeTailContextView() (*coreruntime.JobMemory, []byte, error) {
	if _, err := o.nativeOwnerCodeView(false); err != nil {
		return nil, nil, err
	}
	if !rv64NativeOwnerContextValid(o.instance) {
		return nil, nil, fmt.Errorf("%w: RV64 native tail context has no retained arena owner", railshot.ErrUnsupported)
	}
	// Validation above proves this exact first arena allocation. Preserve its
	// slice provenance instead of reconstructing it from the integer address.
	return o.instance.jm, o.instance.ar.AllocatedPrefix(railshotNativeInvocationContextBytes), nil
}

// Import-only relays retain a mapped cache and native import thunks, but have
// no executable guest span. Owner-graph validation may accept that empty view;
// tail certificates still require the nonempty NativeTailCodeView contract.
func (o rv64TailCodeOwner) nativeOwnerCodeView(allowEmpty bool) ([]byte, error) {
	in := o.instance
	if in == nil {
		return nil, fmt.Errorf("%w: RV64 published image has no instance", railshot.ErrUnsupported)
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.resourcesClosed || in.c == nil || in.jm == nil || in.ar == nil || in.base == 0 || in.nativeContext == 0 || in.resourceRefs == 0 && in.invocationState.Load()&instanceInvocationCount == 0 {
		return nil, fmt.Errorf("%w: RV64 published image has no physical instance lease", railshot.ErrUnsupported)
	}
	cc := in.c.loadCodeCache()
	if cc == nil {
		return nil, fmt.Errorf("%w: RV64 published image has no code cache", railshot.ErrUnsupported)
	}
	cc.mu.Lock()
	defer cc.mu.Unlock()
	b := in.c.code
	indexes := in.c.loadCompileIndexes()
	// Closing the execution snapshot itself clears its readable code slice,
	// while the particular instance's cache reference keeps the RX mapping live.
	// Recover only that mapping's recorded logical span, never page padding.
	if cc.closed && b == nil && indexes != nil {
		n := indexes.railshotTail.mappedCodeBytes
		if n > 0 && n <= len(cc.mem) {
			b = cc.mem[:n:n]
		}
	}
	// Logical Close is allowed: this particular instance still owns its job,
	// arena/context and code reference. A shared cache's refs alone cannot prove
	// that those resources survive for this instance.
	if !cc.sealed || cc.refs <= 0 || cc.base != in.base || indexes == nil || len(b) != indexes.railshotTail.mappedCodeBytes || len(b) > len(cc.mem) || offHeapSlicePtr(cc.mem) != cc.base {
		return nil, fmt.Errorf("%w: RV64 published image differs from sealed code cache", railshot.ErrUnsupported)
	}
	if len(b) == 0 {
		if allowEmpty && in.c.NumImports > 0 && len(in.c.Funcs) == 0 && len(in.c.Entry) == 0 && len(in.c.InternalEntry) == 0 && !in.c.nativeReferenceFunctionABI && !in.c.nativeReferenceCallABI && !in.c.nativeReferenceControlABI && rv64NativeTailModule(in.c) == nil {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: RV64 owner has no executable guest span", railshot.ErrUnsupported)
	}
	if offHeapSlicePtr(b) != cc.base {
		return nil, fmt.Errorf("%w: RV64 published code has a different base", railshot.ErrUnsupported)
	}
	return b[:len(b):len(b)], nil
}
