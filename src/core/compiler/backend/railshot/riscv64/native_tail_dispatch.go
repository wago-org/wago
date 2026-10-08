//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"errors"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// NativeTailCodeRange is a physically retained caller image containing ordinary
// linked-call continuations. It cannot authorize arbitrary process addresses.
type NativeTailCodeRange struct{ Base, Bytes uintptr }

// NativeTailSite describes one immutable module-local return_call site after
// exact source signature and authentic defined target attachment checks. Both
// contexts, homes, target/caller images and their physical owners must remain
// retained by the instance/entry lease through native execution, host parks and
// cold decoration. HostEntry selects a retained synchronous RV64 host wrapper
// in the caller's own home/context. It never accepts a raw Go function address.
// A reexport bridge is not a proper-tail target.
// No process addresses from this descriptor may enter a persisted artifact.
type NativeTailSite struct {
	// dynamic is restricted to the package-private native-only proof. No public
	// source/product/artifact option admits dynamic tails at this boundary.
	dynamic                              *tailRADynamicSite
	CallerHome                           uintptr
	CallerContext, TargetContext         []byte
	Target                               NativeImportTarget
	HostEntry                            bool
	FrameBytes, SourceBase, SourceStride int
	FunctionIndex, SourcePC              uint32
	Parameters, Results                  []wasm.ValType
	Continuations                        []NativeTailCodeRange
}

// NativeTailDispatcher owns published factory code and capability storage.
// The owning instance binds it only after successful publication and closes it
// only on final physical retirement, after all native/parked entry leases have
// drained. It does not acquire an independent per-call lease or native Go root.
type NativeTailDispatcher struct {
	code       *coreruntime.CodeBuffer
	arena      *coreruntime.Arena
	capability []byte
	contexts   [][]byte
	dynamic    []*tailRADynamicSite
	bound      bool
}

// NativeTailInstanceBudget covers a module-local factory with one retained
// caller image per site. Zero imported sites needs no dispatcher allocation.
func NativeTailInstanceBudget(count, guestCodeBytes int) (codeBytes, arenaBytes int, err error) {
	if count < 0 || count > tailRAMaxSites || guestCodeBytes < 0 || guestCodeBytes > MaxCodeBytes {
		return 0, 0, unsupported("native tail instance budget")
	}
	if count == 0 {
		return 0, 0, nil
	}
	codeBytes = count * (tailRADispatchBound + 128)
	if guestCodeBytes == 0 || codeBytes > MaxCodeBytes-guestCodeBytes {
		return 0, 0, unsupported("native tail combined instance code budget")
	}
	return codeBytes, tailRACapHeader + count*tailRASiteRow, nil
}

// NativeTailMetadataBudget charges copied signatures and one retained caller
// range per module-local site before allocating per-site type slices.
func NativeTailMetadataBudget(count, values int) error {
	if count < 0 || count > tailRAMaxSites || values < 0 || values > 512*count {
		return unsupported("native tail metadata count budget")
	}
	size := tailRACapHeader + count*(tailRASiteRow+128+16) + 16*values
	if size > MaxModuleBytes {
		return unsupported("native tail factory metadata bound")
	}
	return nil
}

func nativeTailProofSite(s NativeTailSite) tailRAProofSite {
	ranges := make([]tailRARange, len(s.Continuations))
	for n, r := range s.Continuations {
		ranges[n] = tailRARange{r.Base, r.Bytes}
	}
	return tailRAProofSite{dynamic: s.dynamic, invocationContext: true, hostEntry: s.HostEntry, frameBytes: s.FrameBytes, functionIndex: s.FunctionIndex, sourcePC: s.SourcePC, callerHome: s.CallerHome, callerContext: tailRAPointer(s.CallerContext), target: nativeImportTarget{entry: s.Target.Entry, home: s.Target.Home, context: tailRAPointer(s.TargetContext), hostTrampoline: s.Target.HostTrampoline}, params: s.Parameters, results: s.Results, sourceBase: s.SourceBase, sourceStride: s.SourceStride, continuationRanges: ranges}
}

// Validate all aggregate bounds and source homes before allocating native data,
// mapping factory code or materializing per-site instruction sequences.
func nativeTailDispatcherBound(sites []NativeTailSite, guestCodeBytes int) (int, error) {
	if len(sites) == 0 || len(sites) > tailRAMaxSites || guestCodeBytes <= 0 || guestCodeBytes > MaxCodeBytes {
		return 0, unsupported("native tail factory count/code bound")
	}
	metadata := tailRACapHeader + len(sites)*tailRASiteRow
	code := 0
	for _, s := range sites {
		if s.CallerHome != sites[0].CallerHome || tailRAPointer(s.CallerContext) != tailRAPointer(sites[0].CallerContext) || len(s.CallerContext) != abi.RV64InstanceContextBytes || len(s.TargetContext) != abi.RV64InstanceContextBytes || tailRAPointer(s.CallerContext)&7 != 0 || tailRAPointer(s.TargetContext)&7 != 0 || s.Target.Context != tailRAPointer(s.TargetContext) || !s.Target.InvocationContext || !s.Target.StackOwnedActivation || s.FunctionIndex >= 2*MaxFunctions || binary.LittleEndian.Uint64(s.CallerContext[abi.RV64TailCapabilityOffset:]) != 0 || len(s.Parameters) > 256 || len(s.Results) > 256 || len(s.Continuations) == 0 || len(s.Continuations) > tailRAMaxRanges {
			return 0, unsupported("native tail retained public contexts/target")
		}
		// Account copied signatures and continuation records, not just native rows.
		cost := 128 + 16*(len(s.Parameters)+len(s.Results)+len(s.Continuations))
		dynamicMetadata, dynamicCode, err := s.dynamic.budget()
		if err != nil {
			return 0, err
		}
		cost += dynamicMetadata
		if cost > MaxModuleBytes-metadata {
			return 0, unsupported("native tail factory metadata bound")
		}
		metadata += cost
		// Validate ranges and source homes without copying their payloads first.
		p := tailRAProofSite{dynamic: s.dynamic, invocationContext: true, hostEntry: s.HostEntry, frameBytes: s.FrameBytes, callerHome: s.CallerHome, callerContext: tailRAPointer(s.CallerContext), target: nativeImportTarget{entry: s.Target.Entry, home: s.Target.Home, context: s.Target.Context, hostTrampoline: s.Target.HostTrampoline}, params: s.Parameters, results: s.Results, sourceBase: s.SourceBase, sourceStride: s.SourceStride, continuationRanges: []tailRARange{{s.Continuations[0].Base, s.Continuations[0].Bytes}}}
		if err := p.validate(); err != nil {
			return 0, err
		}
		for _, r := range s.Continuations {
			if !(tailRARange{r.Base, r.Bytes}).valid(4) {
				return 0, unsupported("native tail retained continuation image")
			}
		}
		bound := tailRADispatchBound + 128*len(s.Continuations) + dynamicCode
		if bound > MaxCodeBytes-guestCodeBytes-code {
			return 0, unsupported("native tail combined guest/factory code bound")
		}
		code += bound
	}
	retainedCode, err := nativeTailDynamicCodeBytes(sites)
	if err != nil {
		return 0, err
	}
	if retainedCode > guestCodeBytes {
		return 0, unsupported("dynamic tail understated retained guest budget")
	}
	return code, nil
}

// NewNativeTailDispatcher returns a cleanup owner on any failure after the
// first allocation. Call Close even when err!=nil and the owner is nonnil.
// Capability words remain zero until every factory byte is sealed executable
// and process-wide Linux instruction-cache synchronization has completed.
func NewNativeTailDispatcher(sites []NativeTailSite, guestCodeBytes int) (*NativeTailDispatcher, error) {
	return NewNativeTailDispatcherInArena(sites, guestCodeBytes, nil)
}

// NewNativeTailDispatcherInArena borrows instance-owned capability storage.
// The caller must close the dispatcher before releasing that arena. A nil
// arena selects the independently owned storage used by standalone factories.
func NewNativeTailDispatcherInArena(sites []NativeTailSite, guestCodeBytes int, arena *coreruntime.Arena) (*NativeTailDispatcher, error) {
	bound, err := nativeTailDispatcherBound(sites, guestCodeBytes)
	if err != nil {
		return nil, err
	}
	// Private dynamic targets are borrowed physical owners. Publish every actual
	// framed target before a capability can expose it; their owners retain failed
	// Seal mappings for cleanup. All aggregate checks above precede these calls.
	for _, s := range sites {
		if s.dynamic != nil {
			for _, owner := range s.dynamic.owners {
				for i := 0; i < len(owner.functions)+len(owner.targets); i++ {
					f := owner.retainedFunction(i)
					if err := f.publishCode(); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	exit, err := nativeTailExitPointer()
	if err != nil {
		return nil, err
	}
	o := new(NativeTailDispatcher)
	o.code, err = coreruntime.NewCodeBuffer(bound)
	if err != nil {
		return o, err
	}
	base := o.code.Base()
	if arena == nil {
		o.arena, err = coreruntime.NewArena(tailRACapHeader + len(sites)*tailRASiteRow)
		if err != nil {
			return o, err
		}
		arena = o.arena
	}
	o.capability = arena.Alloc(tailRACapHeader + len(sites)*tailRASiteRow)
	clear(o.capability)
	binary.LittleEndian.PutUint64(o.capability, tailRACapMagic)
	binary.LittleEndian.PutUint32(o.capability[8:], tailRAVersion)
	binary.LittleEndian.PutUint32(o.capability[12:], uint32(len(sites)))
	for n, s := range sites {
		p := nativeTailProofSite(s)
		p.dynamic = s.dynamic.freeze()
		if p.dynamic != nil {
			o.dynamic = append(o.dynamic, p.dynamic)
		}
		p.params = append([]wasm.ValType(nil), s.Parameters...)
		p.results = append([]wasm.ValType(nil), s.Results...)
		final, err := tailRAFinalize(p, exit)
		if err != nil {
			return o, err
		}
		finalAddress := o.code.Base() + uintptr(len(o.code.Bytes()))
		if err = o.code.Append(final); err != nil {
			return o, err
		}
		for len(o.code.Bytes())&15 != 0 {
			if err = o.code.Append([]byte{0x13, 0, 0, 0}); err != nil {
				return o, err
			}
		}
		preAddress := o.code.Base() + uintptr(len(o.code.Bytes()))
		pre, err := tailRAPreflight(p, exit, finalAddress)
		if err != nil {
			return o, err
		}
		_, dynamicCode, _ := p.dynamic.budget()
		if len(final)+len(pre)+32 > tailRADispatchBound+128*len(s.Continuations)+dynamicCode {
			return o, unsupported("native tail actual dispatcher bound")
		}
		if err = o.code.Append(pre); err != nil {
			return o, err
		}
		for len(o.code.Bytes())&15 != 0 {
			if err = o.code.Append([]byte{0x13, 0, 0, 0}); err != nil {
				return o, err
			}
		}
		row := o.capability[tailRACapHeader+n*tailRASiteRow:]
		binary.LittleEndian.PutUint64(row, uint64(preAddress))
		binary.LittleEndian.PutUint64(row[8:], uint64(finalAddress))
		found := false
		for _, ctx := range o.contexts {
			if tailRAPointer(ctx) == tailRAPointer(s.CallerContext) {
				found = true
				break
			}
		}
		if !found {
			o.contexts = append(o.contexts, s.CallerContext)
		}
	}
	if o.code.Base() != base || len(o.code.Bytes()) > bound {
		return o, unsupported("native tail stable mapping bound")
	}
	if err = o.code.Seal(); err != nil {
		return o, err
	}
	for _, ctx := range o.contexts {
		binary.LittleEndian.PutUint64(ctx[abi.RV64TailCapabilityOffset:], uint64(tailRAPointer(o.capability)))
	}
	o.bound = true
	return o, nil
}

// Close detaches before releasing either mapping, and retains failed cleanup
// owners for a retry. Final instance retirement must exclude live guest entries.
func (o *NativeTailDispatcher) Close() error {
	if o == nil {
		return nil
	}
	if o.bound {
		for _, ctx := range o.contexts {
			if binary.LittleEndian.Uint64(ctx[abi.RV64TailCapabilityOffset:]) != uint64(tailRAPointer(o.capability)) {
				return unsupported("native tail capability binding replaced")
			}
		}
		for _, ctx := range o.contexts {
			binary.LittleEndian.PutUint64(ctx[abi.RV64TailCapabilityOffset:], 0)
		}
		o.bound = false
	}
	// Detached owners retry only mappings; they must not retain borrowed context
	// or capability slices after the instance releases its arena.
	o.contexts = nil
	o.dynamic = nil
	if o.arena == nil {
		o.capability = nil
	}
	var errs []error
	if o.code != nil {
		if err := o.code.Close(); err != nil {
			errs = append(errs, err)
		} else {
			o.code = nil
		}
	}
	if o.arena != nil {
		if err := o.arena.Close(); err != nil {
			errs = append(errs, err)
		} else {
			o.arena = nil
			o.capability = nil
			o.contexts = nil
		}
	}
	return errors.Join(errs...)
}
