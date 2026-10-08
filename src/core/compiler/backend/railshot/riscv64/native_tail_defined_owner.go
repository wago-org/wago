//go:build linux && !tinygo

package riscv64

import (
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// NativeTailDefinedOwner is a runtime-only snapshot of a canonical function
// arena whose indexed rows all name genuine defined framed functions. Imported
// proxies and host rows need their own authenticated ownership adapter. The
// caller retains this exact instance's code/job/arena through the execution
// lease, including parks, refreshed factories and cold decoration.
type NativeTailDefinedOwner struct{ owner tailRADynamicOwner }

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
	size := int(unsafe.Sizeof(NativeTailDefinedOwner{})) + count*(int(unsafe.Sizeof(tailRAFunction{}))+2*int(unsafe.Sizeof(wasm.CompType{}))+8) + 2*values*int(unsafe.Sizeof(wasm.ValType{}))
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
	n := len(signatures)
	if n == 0 || n > MaxFunctions || len(entries) < n || len(entries) > 2*MaxFunctions || image == nil || job == nil || len(context) != abi.RV64InstanceContextBytes || len(descriptors) != 40*(n+1) || len(directory) != abi.RV64TailTargetsHeaderBytes+abi.RV64TailTargetsRowBytes*n {
		return nil, unsupported("native defined tail owner count/extent")
	}
	if err := image.Validate(); err != nil {
		return nil, err
	}
	values := 0
	if err := NativeTailDefinedOwnerBudget(n, values); err != nil {
		return nil, err
	}
	code := image.Bytes()
	for i := range signatures {
		sig := &signatures[i]
		if err := tailRASignature(sig); err != nil {
			return nil, err
		}
		values += len(sig.Params) + len(sig.Results)
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
	o := &NativeTailDefinedOwner{owner: tailRADynamicOwner{descriptors: descriptors, context: context, directory: directory, functions: make([]tailRAFunction, n), signatureKeys: make([]uint64, n)}}
	for i, sig := range signatures {
		entry, end := entries[i], len(code)
		if i+1 < len(entries) {
			end = entries[i+1]
		}
		frame, _ := NativeFrameBytes(code[:end], entry)
		sig.Params = append([]wasm.ValType(nil), sig.Params...)
		sig.Results = append([]wasm.ValType(nil), sig.Results...)
		o.owner.signatureKeys[i] = wasm.StructuralFuncTypeKey(&sig)
		info := FunctionInfo{Entry: entry, CodeBytes: end - entry, FrameBytes: frame, FrameHeaderBytes: callFrameHeader, Params: swarSignatureSlots(sig.Params), Results: swarSignatureSlots(sig.Results), mode: emissionMode{production: true, interruptible: true, invocationContext: true, functionIndex: uint32(i)}}
		o.owner.functions[i] = tailRAFunction{published: image, snapshot: code, info: info, signature: &sig, job: job, context: context, directory: descriptors}
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	return o, nil
}

// Validate authenticates canonical rows against retained code and signature
// metadata. It must run under the reacquired particular owner lease on resume.
func (o *NativeTailDefinedOwner) Validate() error {
	if o == nil {
		return unsupported("native defined tail owner is nil")
	}
	list := [1]tailRADynamicOwner{o.owner}
	d := tailRADynamicSite{owners: list[:]}
	if _, _, err := d.budget(); err != nil {
		return err
	}
	return o.owner.validate(0) // Defined rows use no host or ordinary proxy transport.
}

// NativeTailDefinedSite attaches a bounded set of retained defined-owner
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
