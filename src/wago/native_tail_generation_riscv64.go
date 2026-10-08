//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

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

// Private fixed-graph qualification boundary. Public source/artifact admission
// and ordinary entry hooks do not construct or activate these generations yet.
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

// Plans are trusted compiler-side symbolic input. This private attachment proof
// checks signatures, exact actual frames and all owner identities; it does not
// establish that an arbitrary supplied SourcePC contains a decoded tail opcode.
// Public integration must take plans only from frozen validated compiler/loader
// metadata, and pass its charged resource limits. No plan contains an address.
// A nonnil generation returned with an error owns cleanup and budget charges.
func rv64NewDefinedTailGeneration(caller *Instance, scope *rv64NativeTableTargets, plans []encoder.NativeTailPlan, limits rv64NativeTailGenerationLimits) (*rv64NativeTailGeneration, error) {
	if caller == nil || scope == nil || scope.count <= 0 || scope.count > 128 || len(plans) == 0 || len(plans) > 4096 || limits.code <= 0 || limits.code > railshot.MaxCodeBytes || limits.metadata <= 0 || limits.metadata > railshot.MaxModuleBytes {
		return nil, rv64TailGenerationError("count/limit")
	}
	callerIndex, ownerMetadata, guestCode := -1, 0, 0
	functions, ownerValues, rangeCount := 0, 0, 0
	for i, owner := range scope.owners[:scope.count] {
		if owner == caller {
			callerIndex = i
		}
		n, values, err := rv64NativeDefinedTailOwnerCounts(owner)
		if err != nil {
			return nil, err
		}
		charge, err := railshot.NativeTailDefinedOwnerBytes(n, values)
		if err != nil || charge > railshot.MaxModuleBytes-ownerMetadata {
			return nil, rv64TailGenerationError("aggregate owner metadata")
		}
		ownerMetadata += charge
		functions += n
		ownerValues += values
		view, err := (rv64TailCodeOwner{owner}).NativeTailCodeView()
		if err != nil {
			return nil, err
		}
		seen := false
		for _, prior := range scope.owners[:i] {
			if prior == owner {
				return nil, rv64TailGenerationError("duplicate owner")
			}
			if prior.base == owner.base {
				seen = true
			}
		}
		if !seen {
			if len(view) > railshot.MaxCodeBytes-guestCode {
				return nil, rv64TailGenerationError("aggregate guest code")
			}
			guestCode += len(view)
			rangeCount++
		}
	}
	if callerIndex < 0 {
		return nil, rv64TailGenerationError("caller absent from physical scope")
	}
	if err := rv64ValidateNativeTailPlans(caller.c, plans); err != nil {
		return nil, err
	}
	values := 0
	for _, p := range plans {
		if p.Kind != encoder.NativeTailIndirect && p.Kind != encoder.NativeTailReference || int(p.FunctionIndex) >= len(caller.c.Entry) {
			return nil, rv64TailGenerationError("definition-only dynamic plan")
		}
		frame, err := railshot.NativeFrameBytes(caller.c.code, caller.c.Entry[p.FunctionIndex])
		if err != nil || frame != p.FrameBytes {
			return nil, rv64TailGenerationError("actual source frame")
		}
		sig, err := rv64NativeTailPlanSignature(caller.c, p)
		if err != nil {
			return nil, err
		}
		values += sig.paramCount() + sig.resultCount()
	}
	metadata, code, err := railshot.NativeTailDefinedFactoryCountsBudget(len(plans), values, rangeCount, scope.count, functions, ownerValues, guestCode)
	if err != nil {
		return nil, err
	}
	metadata += ownerMetadata + int(unsafe.Sizeof(rv64NativeTailGeneration{})) + scope.count*16
	// Conservatively charge every retained guest image per live generation.
	// Distinct overlapping scopes can contain different images; subtracting
	// only the newest graph's guests would undercharge their retained union.
	code += guestCode
	state := &caller.ensurePluginState().railshotInvocation
	pool := &state.tailGenerationBudget
	if scope.tailBudget.count != 0 && scope.tailBudget.limits != limits || pool.count != 0 && pool.limits != limits {
		return nil, rv64TailGenerationError("overlapping configured limit identity")
	}
	if metadata > limits.metadata-scope.tailBudget.metadata || code > limits.code-scope.tailBudget.code || metadata > limits.metadata-pool.metadata || code > limits.code-pool.code || scope.tailBudget.count == rv64MaxTailGenerations || pool.count == rv64MaxTailGenerations {
		return nil, rv64TailGenerationError("aggregate generation reservation")
	}
	g := &rv64NativeTailGeneration{caller: caller, scope: scope, state: state, codeBytes: code, metadataBytes: metadata, reserved: true}
	for _, budget := range []*rv64NativeTailGenerationBudget{&scope.tailBudget, pool} {
		budget.limits = limits
		budget.code += code
		budget.metadata += metadata
		budget.count++
	}
	// Snapshot construction is cold and bounded across all owners before any
	// signature conversion. Cached snapshots still authenticate every live row.
	var snapshotStorage [128]*railshot.NativeTailDefinedOwner
	var rangeStorage [128]railshot.NativeTailCodeRange
	snapshots, ranges := snapshotStorage[:scope.count], rangeStorage[:0]
	for i, owner := range scope.owners[:scope.count] {
		snapshot, err := rv64NativeDefinedTailOwner(owner)
		if err != nil {
			return g, err
		}
		snapshots[i] = snapshot
		seen := false
		for _, r := range ranges {
			if r.Base == owner.base {
				seen = true
			}
		}
		if !seen {
			ranges = append(ranges, railshot.NativeTailCodeRange{Base: owner.base, Bytes: uintptr(len(owner.c.code))})
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
		site := railshot.NativeTailSite{CallerHome: caller.jm.LinMemBase(), CallerContext: context, TargetContext: context, Target: railshot.NativeImportTarget{Entry: caller.base + uintptr(caller.c.Entry[p.FunctionIndex]), Home: caller.jm.LinMemBase(), Context: caller.nativeContext, HostTrampoline: trampoline, StackOwnedActivation: true, InvocationContext: true}, FrameBytes: p.FrameBytes, SourceBase: p.SourceBase, SourceStride: p.SourceStride, FunctionIndex: p.FunctionIndex, SourcePC: p.SourcePC, Parameters: pt, Results: rt, Continuations: ranges}
		sites[i], err = railshot.NativeTailDefinedSite(site, p.ReferenceBase, snapshots)
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
				if err := g.snapshots[i].Validate(); err != nil {
					return err
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
	for _, budget := range []*rv64NativeTailGenerationBudget{&g.scope.tailBudget, &g.state.tailGenerationBudget} {
		budget.code -= g.codeBytes
		budget.metadata -= g.metadataBytes
		budget.count--
		if budget.count == 0 {
			budget.limits = rv64NativeTailGenerationLimits{}
		}
	}
	g.factory, g.owners, g.snapshots, g.callerOwner = nil, nil, nil, nil
	g.reserved = false
	return nil
}
