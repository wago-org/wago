//go:build linux && !tinygo

package riscv64

import (
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// NativeTailOwner is an immutable runtime snapshot of a canonical indexed
// function arena. Its exact physical owners remain leased through execution,
// parks, refreshed factories and cold decoration. Imported rows retain both
// their ordinary bridge identity and their genuine framed native target.
type NativeTailOwner struct {
	owner      tailRADynamicOwner
	trampoline uintptr
}

// NativeTailDefinedOwner preserves the definition-only constructor contract.
type NativeTailDefinedOwner = NativeTailOwner

// NativeTailDefinedOwnerBudget bounds both staging signatures and retained
// owner metadata before the runtime converts any per-function type slices.
func NativeTailDefinedOwnerBudget(count, values int) error {
	_, err := NativeTailDefinedOwnerBytes(count, values)
	return err
}

// NativeTailDefinedOwnerBytes also exposes the conservative staging/retained
// metadata charge so the runtime can bound a whole graph before constructing
// any of its per-instance snapshots.
func NativeTailDefinedOwnerBytes(count, values int) (int, error) {
	if count <= 0 || count > MaxFunctions || values < 0 || values > 512*count {
		return 0, unsupported("native defined tail owner metadata count")
	}
	size := int(unsafe.Sizeof(NativeTailDefinedOwner{})) + int(unsafe.Sizeof(NativeTailCodeImage{})) + count*(int(unsafe.Sizeof(tailRAFunction{}))+2*int(unsafe.Sizeof(wasm.CompType{}))+8) + 2*values*int(unsafe.Sizeof(wasm.ValType{}))
	if size > MaxModuleBytes {
		return 0, unsupported("native defined tail owner metadata bound")
	}
	return size, nil
}

// NewNativeTailDefinedOwner derives frames and entries from a retained compiler
// image and frozen runtime signatures. entries may include later compiler
// helpers without descriptor rows; each indexed function's span ends at the
// next actual entry. No process address or function metadata is serialized.
func NewNativeTailDefinedOwner(image *NativeTailCodeImage, job *coreruntime.JobMemory, context, descriptors, directory []byte, entries []int, signatures []wasm.CompType) (*NativeTailDefinedOwner, error) {
	return newNativeTailDefinedOwner(image, job, context, descriptors, directory, entries, len(signatures), func(i int) *wasm.CompType { return &signatures[i] }, false)
}

// NewNativeTailSparseDefinedOwner preserves every function ordinal and physical
// frame. A nil signature marks an internal function that is not a descriptor
// target. Its full descriptor and target rows must be zero. The runtime proves
// the closed module's escape rules before supplying this private owner contract.
// Numeric signatures are copied; nil entries become a frozen ordinal mask.
func NewNativeTailSparseDefinedOwner(image *NativeTailCodeImage, job *coreruntime.JobMemory, context, descriptors, directory []byte, entries []int, signatures []*wasm.CompType) (*NativeTailDefinedOwner, error) {
	return newNativeTailDefinedOwner(image, job, context, descriptors, directory, entries, len(signatures), func(i int) *wasm.CompType { return signatures[i] }, true)
}

// Sparse construction charges the pointer input and ordinal mask in addition
// to the conservative dense staging/retained bound. Callable values alone may
// consume signature storage; full row/code extents still use count.
func NativeTailSparseDefinedOwnerBytes(count, nonCallable, values int) (int, error) {
	if nonCallable < 0 || nonCallable > count || values < 0 || count <= 0 || count > MaxFunctions || values > 512*(count-nonCallable) {
		return 0, unsupported("native sparse defined owner counts")
	}
	size, err := NativeTailDefinedOwnerBytes(count, values)
	if err != nil {
		return 0, err
	}
	size += count * (int(unsafe.Sizeof((*wasm.CompType)(nil))) + int(unsafe.Sizeof(false)))
	if size > MaxModuleBytes {
		return 0, unsupported("native sparse defined owner metadata bound")
	}
	return size, nil
}

func newNativeTailDefinedOwner(image *NativeTailCodeImage, job *coreruntime.JobMemory, context, descriptors, directory []byte, entries []int, n int, signature func(int) *wasm.CompType, sparse bool) (*NativeTailDefinedOwner, error) {
	if n == 0 || n > MaxFunctions || len(entries) < n || len(entries) > 2*MaxFunctions || image == nil || job == nil || len(context) != abi.RV64InstanceContextBytes || len(descriptors) != 40*(n+1) || len(directory) != abi.RV64TailTargetsHeaderBytes+abi.RV64TailTargetsRowBytes*n {
		return nil, unsupported("native defined tail owner count/extent")
	}
	if err := image.Validate(); err != nil {
		return nil, err
	}
	values, nonCallable := 0, 0
	if err := NativeTailDefinedOwnerBudget(n, values); err != nil {
		return nil, err
	}
	code := image.Bytes()
	for i := 0; i < n; i++ {
		sig := signature(i)
		if sparse && sig == nil {
			nonCallable++
		} else {
			if err := tailRASignature(sig); err != nil {
				return nil, err
			}
			if len(sig.Fields) != 0 || sig.Array != (wasm.FieldType{}) {
				return nil, unsupported("native defined tail inactive signature metadata")
			}
			values += len(sig.Params) + len(sig.Results)
		}
		if err := NativeTailDefinedOwnerBudget(n, values); err != nil {
			return nil, err
		}
		entry, end := entries[i], len(code)
		if i+1 < len(entries) {
			end = entries[i+1]
		}
		if entry < 0 || entry&15 != 0 || end <= entry || end > len(code) || end&3 != 0 {
			return nil, unsupported("native defined tail owner function span")
		}
		if _, err := NativeFrameBytes(code[:end], entry); err != nil {
			return nil, err
		}
	}
	if sparse {
		if _, err := NativeTailSparseDefinedOwnerBytes(n, nonCallable, values); err != nil {
			return nil, err
		}
	}
	o := &NativeTailDefinedOwner{owner: tailRADynamicOwner{descriptors: descriptors, context: context, directory: directory, functions: make([]tailRAFunction, n), signatureKeys: make([]uint64, n)}}
	if sparse {
		o.owner.uncallable = make([]bool, n)
	}
	for i := 0; i < n; i++ {
		entry, end := entries[i], len(code)
		if i+1 < len(entries) {
			end = entries[i+1]
		}
		frame, _ := NativeFrameBytes(code[:end], entry)
		info := FunctionInfo{Entry: entry, CodeBytes: end - entry, FrameBytes: frame, FrameHeaderBytes: callFrameHeader, mode: emissionMode{production: true, interruptible: true, invocationContext: true, functionIndex: uint32(i)}}
		var frozen *wasm.CompType
		if sig := signature(i); sig != nil {
			// Do not retain inactive empty slice backing storage from the input.
			copy := wasm.CompType{Kind: wasm.CompFunc}
			copy.Params = append([]wasm.ValType(nil), sig.Params...)
			copy.Results = append([]wasm.ValType(nil), sig.Results...)
			frozen = &copy
			o.owner.signatureKeys[i] = wasm.StructuralFuncTypeKey(frozen)
			info.Params, info.Results = swarSignatureSlots(sig.Params), swarSignatureSlots(sig.Results)
		} else {
			o.owner.uncallable[i] = true
		}
		o.owner.functions[i] = tailRAFunction{published: image, snapshot: code, info: info, signature: frozen, job: job, context: context, directory: descriptors}
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	return o, nil
}

// Validate authenticates canonical rows against retained code and signature
// metadata. It must run under the reacquired particular owner lease on resume.
func (o *NativeTailOwner) Validate() error {
	if o == nil {
		return unsupported("native defined tail owner is nil")
	}
	list := [1]tailRADynamicOwner{o.owner}
	d := tailRADynamicSite{owners: list[:]}
	if _, _, err := d.budget(); err != nil {
		return err
	}
	return o.owner.validate(o.trampoline)
}

// NativeTailDefinedSite attaches a bounded set of retained native-owner
// snapshots to one actual source plan. Factory construction still performs the
// complete aggregate budget/publication check before allocating native storage.
// This internal runtime contract does not change public feature admission.
func NativeTailDefinedSite(s NativeTailSite, referenceBase int, owners []*NativeTailDefinedOwner) (NativeTailSite, error) {
	if s.dynamic != nil || len(owners) == 0 || len(owners) > tailRAMaxRanges || len(s.Continuations) == 0 || len(s.Continuations) > tailRAMaxRanges || len(s.Parameters) > 256 || len(s.Results) > 256 {
		return NativeTailSite{}, unsupported("native defined tail site count/shape")
	}
	var retained [tailRAMaxRanges]tailRADynamicOwner
	for i, owner := range owners {
		if owner == nil {
			return NativeTailSite{}, unsupported("native defined tail site missing owner")
		}
		retained[i] = owner.owner
	}
	d := tailRADynamicSite{referenceBase: referenceBase, key: wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: s.Parameters, Results: s.Results}), owners: retained[:len(owners)]}
	if _, _, err := d.budget(); err != nil {
		return NativeTailSite{}, err
	}
	// Validate source homes and every owner before copying retained records.
	p := tailRAProofSite{dynamic: &d, invocationContext: true, hostEntry: s.HostEntry, frameBytes: s.FrameBytes, callerHome: s.CallerHome, callerContext: tailRAPointer(s.CallerContext), target: nativeImportTarget{entry: s.Target.Entry, home: s.Target.Home, context: s.Target.Context, hostTrampoline: s.Target.HostTrampoline}, params: s.Parameters, results: s.Results, sourceBase: s.SourceBase, sourceStride: s.SourceStride, continuationRanges: []tailRARange{{s.Continuations[0].Base, s.Continuations[0].Bytes}}}
	if err := p.validate(); err != nil {
		return NativeTailSite{}, err
	}
	d.owners = append([]tailRADynamicOwner(nil), d.owners...)
	s.dynamic = &d
	return s, nil
}
