//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// A generation freezes one retained owner graph. Entry/resume replaces it only
// when that graph grows; older versions survive through the entry's cold exit.
// Every operation is serialized by nativeExecutionMu. The exact scope keeps all
// original physical roots through parks, other roots' entries and cold handling.
const rv64MaxTailGenerations = 32

type rv64NativeTailGenerationBudget struct {
	code, metadata, count int
	limits                rv64NativeTailGenerationLimits
}
type rv64NativeTailGenerationLimits struct{ code, metadata int }

type rv64NativeTailGeneration struct {
	caller        *Instance
	scope         *rv64NativeTableTargets
	state         *railshotInvocationState
	hostScope     hostCallScope
	factory       *railshot.NativeTailDispatcher
	owners        []*Instance
	snapshots     []*railshot.NativeTailDefinedOwner
	callerOwner   *railshot.NativeTailDefinedOwner
	codeBytes     int
	metadataBytes int
	reserved      bool
	ready         bool
}

func rv64TailGenerationError(detail string) error {
	return fmt.Errorf("%w: RV64 tail generation %s", railshot.ErrUnsupported, detail)
}

// Plans are trusted compiler-side symbolic input. This runtime attachment check
// checks signatures, exact actual frames and all owner identities; it does not
// establish that an arbitrary supplied SourcePC contains a decoded tail opcode.
// Public integration must take plans only from frozen validated compiler/loader
// metadata, and pass its charged resource limits. No plan contains an address.
// A nonnil generation returned with an error owns cleanup and budget charges.
func rv64NewDefinedTailGeneration(caller *Instance, scope *rv64NativeTableTargets, plans []encoder.NativeTailPlan, limits rv64NativeTailGenerationLimits) (*rv64NativeTailGeneration, error) {
	return rv64NewDefinedTailGenerationWithBudgets(caller, scope, plans, limits, limits)
}

func rv64NewDefinedTailGenerationWithBudgets(caller *Instance, scope *rv64NativeTableTargets, plans []encoder.NativeTailPlan, scopeLimits, callerLimits rv64NativeTailGenerationLimits) (*rv64NativeTailGeneration, error) {
	if caller == nil || scope == nil || scope.count <= 0 || scope.count > 128 || len(plans) == 0 || len(plans) > 4096 || scopeLimits.code < 0 || scopeLimits.code > railshot.MaxCodeBytes || scopeLimits.metadata < 0 || scopeLimits.metadata > railshot.MaxModuleBytes || callerLimits.code < 0 || callerLimits.code > railshot.MaxCodeBytes || callerLimits.metadata < 0 || callerLimits.metadata > railshot.MaxModuleBytes {
		return nil, rv64TailGenerationError("count/limit")
	}
	callerIndex, ownerMetadata, guestCode := -1, 0, 0
	var callerCode []byte
	var counts railshot.NativeTailOwnerCounts
	var ownerCounts [128]railshot.NativeTailOwnerCounts
	var rangeStorage [128]railshot.NativeTailCodeRange
	ranges := rangeStorage[:0]
	addRange := func(view []byte) error {
		base := offHeapSlicePtr(view)
		for _, prior := range ranges {
			if prior.Base == base {
				if prior.Bytes != uintptr(len(view)) {
					return rv64TailGenerationError("shared image extent changed")
				}
				return nil
			}
		}
		if len(ranges) == len(rangeStorage) || len(view) > railshot.MaxCodeBytes-guestCode {
			return rv64TailGenerationError("aggregate guest image count/bytes")
		}
		guestCode += len(view)
		ranges = append(ranges, railshot.NativeTailCodeRange{Base: base, Bytes: uintptr(len(view))})
		return nil
	}
	for i, owner := range scope.owners[:scope.count] {
		if owner == caller {
			callerIndex = i
		}
		for _, prior := range scope.owners[:i] {
			if prior == owner {
				return nil, rv64TailGenerationError("duplicate owner")
			}
		}
		actual, charge, err := rv64NativeTailOwnerCounts(owner)
		if err != nil {
			return nil, err
		}
		if charge > railshot.MaxModuleBytes-ownerMetadata {
			return nil, rv64TailGenerationError("aggregate owner metadata")
		}
		ownerMetadata += charge
		ownerCounts[i] = actual
		counts.Owners += actual.Owners
		counts.Functions += actual.Functions
		counts.Values += actual.Values
		counts.Targets += actual.Targets
		counts.TargetValues += actual.TargetValues
		counts.ProxySlots += actual.ProxySlots
		counts.Proxies += actual.Proxies
		counts.HostSlots += actual.HostSlots
		counts.MaskSlots += actual.MaskSlots
		counts.NonCallable += actual.NonCallable
		counts.TargetNonCallable += actual.TargetNonCallable
		view, err := (rv64TailCodeOwner{owner}).nativeOwnerCodeView(true)
		if err != nil {
			return nil, err
		}
		if len(view) != 0 {
			if err := addRange(view); err != nil {
				return nil, err
			}
		}
		// Static factories belong to their particular instances, even when guest
		// images are shared. Include their existing publication charge when a
		// static native caller reaches a dynamic callee's generation.
		if owner == caller {
			callerCode = view
		}
		static, _, err := railshot.NativeTailInstanceBudget(rv64StaticTailPlanCount(rv64NativeTailModule(owner.c)), len(view))
		if err != nil {
			return nil, err
		}
		if static > railshot.MaxCodeBytes-guestCode {
			return nil, rv64TailGenerationError("retained static factory code bound")
		}
		guestCode += static
		if state := owner.pluginState.Load(); state != nil && state.railshotInvocation.nativeImports != nil {
			view, err := (rv64TailThunkCodeOwner{owner}).NativeTailCodeView()
			if err != nil {
				return nil, err
			}
			if err := addRange(view); err != nil {
				return nil, err
			}
		}
		// Indexed host rows can share an entire logical cache span. Charge that
		// span once per generation, including nonzero wrapper offsets. Bare
		// producers keep their ordinary host protocol without acquiring rows.
		if actual.HostSlots != 0 {
			for index := 0; index < owner.c.NumImports; index++ {
				if _, native := rv64NativeFunctionAttachment(owner, index); native {
					continue
				}
				host, err := rv64HostTailAttached(owner, index)
				if err != nil {
					return nil, err
				}
				if err := addRange(host.view); err != nil {
					return nil, err
				}
			}
		}
	}
	if callerIndex < 0 || ownerCounts[callerIndex].Owners == 0 {
		return nil, rv64TailGenerationError("caller absent from physical scope")
	}
	if err := rv64ValidateNativeTailPlansWithCodeBytes(caller.c, plans, len(callerCode)); err != nil {
		return nil, err
	}
	values := 0
	for _, p := range plans {
		local := int(p.FunctionIndex) - caller.c.NumImports
		if p.Kind > encoder.NativeTailReference || local < 0 || local >= len(caller.c.Entry) {
			return nil, rv64TailGenerationError("native dynamic plan")
		}
		frame, err := railshot.NativeFrameBytes(callerCode, caller.c.Entry[int(p.FunctionIndex)-caller.c.NumImports])
		if err != nil || frame != p.FrameBytes {
			return nil, rv64TailGenerationError("actual source frame")
		}
		sig, err := rv64NativeTailPlanSignature(caller.c, p)
		if err != nil {
			return nil, err
		}
		values += sig.paramCount() + sig.resultCount()
	}
	metadata, code, err := railshot.NativeTailFactoryCountsBudget(len(plans), values, len(ranges), counts, guestCode)
	if err != nil {
		return nil, err
	}
	metadata += ownerMetadata + int(unsafe.Sizeof(rv64NativeTailGeneration{})) + 2*int(unsafe.Sizeof(rv64RetiredTailGeneration{})) + 32 + scope.count*16
	primitiveTails := (rv64PrimitiveReferenceContract(caller.c) || rv64FreshRootedGC(caller.c)) && rv64HasDynamicTail(rv64NativeTailModule(caller.c))
	if primitiveTails {
		// All typed sites share one retained caller image. Factory budgeting
		// charges the per-site proof fields through tailRADynamicSite's size.
		metadata += 2 * int(unsafe.Sizeof(railshot.NativeTailCodeImage{}))
	}
	// Conservatively charge every retained guest image per live generation.
	// Distinct overlapping scopes can contain different images; subtracting
	// only the newest graph's guests would undercharge their retained union.
	code += guestCode
	state := &caller.ensurePluginState().railshotInvocation
	pool := &state.tailGenerationBudget
	budgets := [2]struct {
		budget *rv64NativeTailGenerationBudget
		limits rv64NativeTailGenerationLimits
	}{{&scope.tailBudget, scopeLimits}, {pool, callerLimits}}
	for i, reservation := range budgets {
		budget, limits := reservation.budget, reservation.limits
		if budget.count != 0 && budget.limits != limits {
			return nil, rv64TailGenerationError("overlapping configured limit identity")
		}
		if metadata > limits.metadata-budget.metadata {
			return nil, &rv64TailReservationError{scope: i == 0, requested: budget.metadata + metadata, limit: limits.metadata}
		}
		if code > limits.code-budget.code {
			return nil, &rv64TailReservationError{scope: i == 0, code: true, requested: budget.code + code, limit: limits.code}
		}
		if budget.count == rv64MaxTailGenerations {
			return nil, rv64TailGenerationError("aggregate generation count")
		}
	}
	g := &rv64NativeTailGeneration{caller: caller, scope: scope, state: state, codeBytes: code, metadataBytes: metadata, reserved: true}
	g.hostScope.bound = true
	for _, reservation := range budgets {
		budget := reservation.budget
		budget.limits = reservation.limits
		budget.code += code
		budget.metadata += metadata
		budget.count++
	}
	// Snapshot construction is cold and bounded across all owners before any
	// signature conversion. Its owned metadata ends with this generation.
	var snapshotStorage [128]*railshot.NativeTailOwner
	var indexedStorage [128]*railshot.NativeTailOwner
	snapshots, indexed := snapshotStorage[:scope.count], indexedStorage[:0]
	for i, owner := range scope.owners[:scope.count] {
		snapshot, err := rv64NewNativeTailOwner(owner, ownerCounts[i])
		if err != nil {
			return g, err
		}
		snapshots[i] = snapshot
		if snapshot != nil {
			indexed = append(indexed, snapshot)
		}
	}
	g.callerOwner = snapshots[callerIndex]
	g.owners = append([]*Instance(nil), scope.owners[:scope.count]...)
	g.snapshots = append([]*railshot.NativeTailDefinedOwner(nil), snapshots...)
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		return g, err
	}
	sites := make([]railshot.NativeTailSite, len(plans))
	var typedImage *railshot.NativeTailCodeImage
	if primitiveTails {
		typedImage, err = railshot.NewNativeTailCodeImage(rv64TailCodeOwner{caller})
		if err != nil {
			return g, err
		}
	}
	for i, p := range plans {
		sig, _ := rv64NativeTailPlanSignature(caller.c, p)
		params, results := make([]ValType, sig.paramCount()), make([]ValType, sig.resultCount())
		for n := range params {
			params[n] = sig.param(n)
		}
		for n := range results {
			results[n] = sig.result(n)
		}
		pt, err := rv64TailTypes(params)
		if err != nil {
			return g, err
		}
		rt, err := rv64TailTypes(results)
		if err != nil {
			return g, err
		}
		context := rv64NativeInvocationContext(caller)
		site := railshot.NativeTailSite{CallerHome: caller.jm.LinMemBase(), CallerContext: context, TargetContext: context, Target: railshot.NativeImportTarget{Entry: caller.base + uintptr(caller.c.Entry[int(p.FunctionIndex)-caller.c.NumImports]), Home: caller.jm.LinMemBase(), Context: caller.nativeContext, HostTrampoline: trampoline, StackOwnedActivation: true, InvocationContext: true}, FrameBytes: p.FrameBytes, SourceBase: p.SourceBase, SourceStride: p.SourceStride, FunctionIndex: p.FunctionIndex, SourcePC: p.SourcePC, Parameters: pt, Results: rt, Continuations: ranges}
		if p.Kind == encoder.NativeTailDirectImport {
			target, native := rv64NativeFunctionAttachment(caller, int(p.ImportIndex))
			if !native {
				// The indexed snapshot already certifies this exact host binding.
				// Reuse its authenticated attachment; allocate no second owner.
				host, err := rv64HostTailAttached(caller, int(p.ImportIndex))
				if err != nil {
					return g, err
				}
				site.HostEntry = true
				site.Target.Entry = offHeapSlicePtr(host.view) + uintptr(host.record.offset)
				sites[i] = site
				continue
			}
			if target == nil {
				return g, rv64TailGenerationError("mixed direct target has no native definition")
			}
			if funcSigHasGCRefs(caller.c.importFuncSigs[p.ImportIndex]) && (caller.gc == nil || caller.gc != target.inst.gc || caller.refStore != target.inst.refStore) {
				return g, rv64TailGenerationError("mixed collector tail target requires the same Runtime collector domain")
			}
			entry, home, ctx, valid := rv64QualifiedNativeTailTarget(target)
			if !valid {
				return g, rv64TailGenerationError("mixed direct target frame")
			}
			site.TargetContext = rv64NativeInvocationContext(target.inst)
			site.ContextOnlyTarget = !target.inst.c.needsFuncRefContext()
			site.Target = railshot.NativeImportTarget{Entry: uintptr(entry), Home: uintptr(home), Context: uintptr(ctx), HostTrampoline: trampoline, StackOwnedActivation: true, InvocationContext: true}
			sites[i] = site
		} else {
			if typedImage != nil {
				sites[i], err = railshot.NativeTailDefinedTypedSite(site, p.ReferenceBase, indexed, typedImage, p.TypeIndex)
			} else {
				sites[i], err = railshot.NativeTailDefinedSite(site, p.ReferenceBase, indexed)
			}
		}
		if err != nil {
			return g, err
		}
	}
	g.factory, err = railshot.NewNativeTailDispatcherDetached(sites, guestCode)
	g.ready = err == nil
	return g, err
}

func (g *rv64NativeTailGeneration) validate() error {
	if g == nil || !g.ready || !g.reserved || g.factory == nil || g.scope.count != len(g.owners) {
		return rv64TailGenerationError("inactive or changed graph")
	}
	if err := g.scope.validateReachable(); err != nil {
		return err
	}
	if g.scope.count != len(g.owners) {
		return rv64TailGenerationError("fixed graph grew on refresh")
	}
	for _, owner := range g.scope.owners[:g.scope.count] {
		found := false
		for i, retained := range g.owners {
			if retained == owner {
				if snapshot := g.snapshots[i]; snapshot != nil {
					if err := snapshot.Validate(); err != nil {
						return err
					}
				} else if _, err := (rv64TailCodeOwner{owner}).nativeOwnerCodeView(true); err != nil {
					return err
				}
				if state := owner.pluginState.Load(); state != nil && state.railshotInvocation.nativeImports != nil {
					if _, err := (rv64TailThunkCodeOwner{owner}).NativeTailCodeView(); err != nil {
						return err
					}
				}
				found = true
				break
			}
		}
		if !found {
			return rv64TailGenerationError("changed owner identity")
		}
	}
	return nil
}

// Validate the complete requested set before replacing any current binding.
// Overlapping scopes may resume in either order. There is no assumed LIFO order
// across roots; each particular instance remembers only its currently bound
// generation, while each suspended scope retains its own generations and roots.
func rv64ActivateNativeTailGenerations(generations []*rv64NativeTailGeneration) error {
	if len(generations) == 0 || len(generations) > 128 {
		return rv64TailGenerationError("activation count")
	}
	for i, g := range generations {
		if err := g.validate(); err != nil {
			return err
		}
		for _, prior := range generations[:i] {
			if prior.caller == g.caller {
				return rv64TailGenerationError("duplicate binding context")
			}
		}
		if current := g.state.activeTailGeneration; current != nil {
			if err := current.factory.CheckDetach(); err != nil {
				return err
			}
		} else if binary.LittleEndian.Uint64(rv64NativeInvocationContext(g.caller)[abi.RV64TailCapabilityOffset:]) != 0 {
			return rv64TailGenerationError("untracked occupied caller slot")
		}
	}
	for _, g := range generations {
		if current := g.state.activeTailGeneration; current != nil {
			if err := current.factory.Detach(); err != nil {
				return err
			}
			g.state.activeTailGeneration = nil
		}
	}
	for i, g := range generations {
		if err := g.factory.Bind(g.callerOwner); err != nil {
			// All contexts stay physically leased under the same native mutex.
			for _, bound := range generations[:i] {
				if detachErr := bound.deactivate(); detachErr != nil {
					return fmt.Errorf("%w; rollback: %v", err, detachErr)
				}
			}
			return err
		}
		g.state.activeTailGeneration = g
	}
	return nil
}

func (g *rv64NativeTailGeneration) deactivate() error {
	if g == nil || g.state.activeTailGeneration != g {
		return nil
	}
	if err := g.factory.Detach(); err != nil {
		return err
	}
	g.state.activeTailGeneration = nil
	return nil
}

// Close runs after the scope's native/suspended/cold uses have ended. It never
// detaches another scope's generation. Failed cleanup retains all budget charges
// and its cleanup owner; no replacement allocation may silently forget them.
func (g *rv64NativeTailGeneration) Close() error {
	if g == nil || !g.reserved {
		return nil
	}
	g.ready = false
	if err := g.deactivate(); err != nil {
		return err
	}
	if err := g.factory.Close(); err != nil {
		return err
	}
	g.releaseReservation()
	return nil
}

func (g *rv64NativeTailGeneration) releaseReservation() {
	var budgets [2]*rv64NativeTailGenerationBudget
	if g.scope != nil {
		budgets[0] = &g.scope.tailBudget
	}
	budgets[1] = &g.state.tailGenerationBudget
	for _, budget := range budgets {
		if budget == nil {
			continue
		}
		budget.code -= g.codeBytes
		budget.metadata -= g.metadataBytes
		budget.count--
		if budget.count == 0 {
			budget.limits = rv64NativeTailGenerationLimits{}
		}
	}
	g.factory, g.owners, g.snapshots, g.callerOwner = nil, nil, nil, nil
	g.scope = nil
	g.reserved = false
}
