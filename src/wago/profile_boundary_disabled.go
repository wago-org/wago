//go:build !wago_profile

package wago

import (
	"context"
	"github.com/wago-org/wago/internal/jitprofile"
)

type profileInstanceState struct{}

func (*Instance) boundaryProfile() *CodeProfile { return nil }
func (*hostLoopActivation) beginProfileActivation() (jitprofile.SpanToken, func()) {
	return jitprofile.SpanToken{}, nil
}
func finishProfileBoundary(jitprofile.SpanToken, *error) {}
func (*hostLoopActivation) callProfiledHost(*Instance, uintptr, uint32, []uint64, []uint64, hostInvocationContext) {
}
func profileScalarFallback(uintptr, uint32, uint32, uint64, uint64) (uint64, bool) { return 0, false }

type profileSpanToken = jitprofile.SpanToken

func profileReentryContext(*Instance, *Instance, context.Context) (context.Context, profileSpanToken) {
	return nil, profileSpanToken{}
}
func inheritProfileContext(invocation hostInvocationContext, _ context.Context) hostInvocationContext {
	return invocation
}
func (*hostLoopActivation) profileHelper(*Instance, string, int) profileSpanToken {
	return profileSpanToken{}
}

type profileInvocationState struct{}

func (hostInvocationContext) profileSession() *CodeProfile { return nil }

func (*Instance) beginProfileInvocation(string) (profileSpanToken, func()) {
	return profileSpanToken{}, nil
}

func (*Instance) beginProfileHostCallback(int) (profileSpanToken, func()) {
	return profileSpanToken{}, nil
}

type profileBuilderState struct{}

func (*instanceBuilder) beginProfileInstantiation() profileSpanToken { return profileSpanToken{} }
func (*instanceBuilder) bindProfileInstance(*Instance)               {}
func (*Instance) lifecycleProfile() *CodeProfile                     { return nil }
func (*Instance) beginProfileLifecycle(string) profileSpanToken      { return profileSpanToken{} }

func (*instanceBuilder) beginProfileInitialization(*Instance, context.Context) (profileSpanToken, context.Context) {
	return profileSpanToken{}, nil
}
