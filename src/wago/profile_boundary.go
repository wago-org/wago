//go:build wago_profile

package wago

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/wago-org/wago/internal/jitprofile"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type profileInstanceState struct{ id atomic.Uint64 }

var nextProfileInstance atomic.Uint64

func (in *Instance) boundaryProfile() *CodeProfile {
	if in == nil || in.c == nil {
		return nil
	}
	cc := in.c.loadCodeCache()
	profile := cc.loadProfile()
	if profile == nil || !profile.session.TraceBoundaries() {
		return nil
	}
	return profile.session
}

func (in *Instance) beginProfileBoundary(session *CodeProfile, invocation hostInvocationContext, kind string, function int) jitprofile.SpanToken {
	if session == nil {
		return jitprofile.SpanToken{}
	}
	id := in.ensurePluginState().profileState.id.Load()
	if id == 0 {
		next := nextProfileInstance.Add(1)
		if in.ensurePluginState().profileState.id.CompareAndSwap(0, next) {
			id = next
		} else {
			id = in.ensurePluginState().profileState.id.Load()
		}
	}
	parent := uint64(0)
	if invocation.traceSession == session {
		parent = invocation.traceParent
	}
	module := ""
	if profile := in.c.loadCodeCache().loadProfile(); profile != nil {
		module = profile.image.ModuleID
	}
	return session.BeginSpan(jitprofile.Span{ParentID: parent, InvocationID: uint64(invocation.id), InstanceID: id, ModuleID: module, Kind: kind, Function: function})
}

func finishProfileBoundary(span jitprofile.SpanToken, err *error) {
	if r := recover(); r != nil {
		outcome := "panic"
		switch trap := r.(type) {
		case HostExit, *HostExit:
			outcome = "host-exit"
		case HostTrap:
			outcome = "trap"
			if errors.Is(trap.Err, context.Canceled) || errors.Is(trap.Err, context.DeadlineExceeded) {
				outcome = "cancelled"
			}
		case *HostTrap:
			outcome = "trap"
			if trap != nil && (errors.Is(trap.Err, context.Canceled) || errors.Is(trap.Err, context.DeadlineExceeded)) {
				outcome = "cancelled"
			}
		case gcStructHelperTrap:
			outcome = "trap"
		case gcStructHelperError, atomicWaitHelperError:
			outcome = "error"
		}
		span.Finish(outcome)
		panic(r)
	}
	outcome := "return"
	if err != nil && *err != nil {
		var trap *coreruntime.TrapError
		var exit *ExitError
		switch {
		case errors.Is(*err, context.Canceled), errors.Is(*err, context.DeadlineExceeded):
			outcome = "cancelled"
		case errors.As(*err, &exit):
			outcome = "host-exit"
		case errors.As(*err, &trap):
			outcome = "trap"
			if trap.Code == coreruntime.TrapInterrupted {
				outcome = "cancelled"
			}
		default:
			outcome = "error"
		}
	}
	span.Finish(outcome)
}

// beginProfileActivation binds tracing ancestry to the same control frame used
// by host invocation authority. This does not acquire or release any lease.
func (a *hostLoopActivation) beginProfileActivation() (jitprofile.SpanToken, func()) {
	invocation := activeHostInvocationContext(a.root)
	session := a.root.boundaryProfile()
	if session == nil {
		session = invocation.traceSession
	}
	span := a.root.beginProfileBoundary(session, invocation, "guest-activation", -1)
	invocation.traceSession = session
	invocation.traceParent = span.ID()
	a.invocation = invocation
	return span, bindHostInvocationContext(a.ctrl, invocation)
}

func (a *hostLoopActivation) callProfiledHost(active *Instance, ctrl uintptr, importIdx uint32, args, results []uint64, invocation hostInvocationContext) {
	session := invocation.traceSession
	if session == nil {
		session = a.root.boundaryProfile()
	}
	span := active.beginProfileBoundary(session, invocation, "host-callback", int(importIdx))
	defer finishProfileBoundary(span, nil)
	invocation.traceSession = session
	invocation.traceParent = span.ID()
	restore := bindHostInvocationContext(ctrl, invocation)
	defer restore()
	if a.root != nil && a.root != active {
		restoreRoot := bindHostInvocationContext(a.ctrl, invocation)
		defer restoreRoot()
	}
	active.callHostDispatch(ctrl, importIdx, args, results, invocation)
}

// Returning unhandled sends an observed prepared call through the generic
// dispatcher, preserving the ordinary lease/rooting protocol and trace hooks.
func profileScalarFallback(uintptr, uint32, uint32, uint64, uint64) (uint64, bool) { return 0, false }

type profileSpanToken = jitprofile.SpanToken
type profileContextKey struct{}

func profileReentryContext(active, target *Instance, ctx context.Context) (context.Context, profileSpanToken) {
	invocation := activeHostInvocationContext(active)
	span := target.beginProfileBoundary(invocation.traceSession, invocation, "guest-invocation", -1)
	invocation.traceParent = span.ID()
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, profileContextKey{}, invocation), span
}

func inheritProfileContext(invocation hostInvocationContext, parent context.Context) hostInvocationContext {
	if parent != nil {
		if traced, ok := parent.Value(profileContextKey{}).(hostInvocationContext); ok {
			invocation.traceSession = traced.traceSession
			invocation.traceParent = traced.traceParent
		}
	}
	return invocation
}

func (a *hostLoopActivation) profileHelper(active *Instance, kind string, function int) profileSpanToken {
	invocation := a.context(active)
	return active.beginProfileBoundary(invocation.traceSession, invocation, kind, function)
}

type profileInvocationState struct {
	traceSession *CodeProfile
	traceParent  uint64
}

func (c hostInvocationContext) profileSession() *CodeProfile { return c.traceSession }

// beginProfileInvocation runs under the admitted instance invocation gate. Its
// identity is the runtime's actual invocation ID, not a synthetic address or PC.
// Gate wait time precedes this span; native transition overhead is included.
func (in *Instance) beginProfileInvocation(export string) (profileSpanToken, func()) {
	invocation := activeHostInvocationContext(in)
	session := in.boundaryProfile()
	function := -1
	if index, ok := in.c.Exports[export]; ok {
		function = index
	}
	span := in.beginProfileBoundary(session, invocation, "guest-invocation", function)
	invocation.traceSession = session
	invocation.traceParent = span.ID()
	return span, bindHostInvocationContext(offHeapSlicePtr(in.ctrl), invocation)
}

func (in *Instance) beginProfileHostCallback(function int) (profileSpanToken, func()) {
	invocation := activeHostInvocationContext(in)
	span := in.beginProfileBoundary(invocation.traceSession, invocation, "host-callback", function)
	invocation.traceParent = span.ID()
	return span, bindHostInvocationContext(offHeapSlicePtr(in.ctrl), invocation)
}

type profileBuilderState struct {
	instanceID uint64
	spanID     uint64
}

func (b *instanceBuilder) beginProfileInstantiation() profileSpanToken {
	if b.c == nil {
		return profileSpanToken{}
	}
	cc := b.c.loadCodeCache()
	if cc == nil {
		return profileSpanToken{}
	}
	cc.mu.Lock()
	profile := cc.loadProfile()
	cc.mu.Unlock()
	if profile == nil || !profile.session.TraceLifecycle() {
		return profileSpanToken{}
	}
	b.instanceID = nextProfileInstance.Add(1)
	span := profile.session.BeginSpan(jitprofile.Span{InstanceID: b.instanceID, ModuleID: profile.image.ModuleID, Kind: "instantiate", Function: -1})
	b.spanID = span.ID()
	return span
}

func (b *instanceBuilder) bindProfileInstance(in *Instance) {
	if b.instanceID != 0 {
		in.ensurePluginState().profileState.id.Store(b.instanceID)
	}
}

func (in *Instance) lifecycleProfile() *CodeProfile {
	session := in.boundaryProfile()
	if session == nil || !session.TraceLifecycle() {
		return nil
	}
	return session
}

// Lifecycle operations may run concurrently with an invocation. Do not read its
// mutable authority or make close/release children of an unrelated call chain.
func (in *Instance) beginProfileLifecycle(kind string) profileSpanToken {
	return in.beginProfileBoundary(in.lifecycleProfile(), hostInvocationContext{}, kind, -1)
}

func (b *instanceBuilder) beginProfileInitialization(in *Instance, parent context.Context) (profileSpanToken, context.Context) {
	session := in.lifecycleProfile()
	function := in.c.NumImports + in.c.StartLocalFunc
	kind := "initialize-guest"
	if in.c.StartIsImport {
		function = in.c.StartImportIdx
		kind = "initialize-host"
	}
	invocation := hostInvocationContext{profileInvocationState: profileInvocationState{traceSession: session, traceParent: b.spanID}}
	span := in.beginProfileBoundary(session, invocation, kind, function)
	invocation.traceParent = span.ID()
	if parent == nil {
		parent = context.Background()
	}
	return span, context.WithValue(parent, profileContextKey{}, invocation)
}
