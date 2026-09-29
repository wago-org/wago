//go:build amd64 && !wago_profile

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

type profileFnState struct{}
type profileOrigin struct{}

func (*fn) recordProfileCodeSite(int, string)         {}
func (*fn) profileCodeSites() []shared.NativeCodeSite { return nil }

func (*fn) rememberProfileNode(*elem)                    {}
func (*fn) enterProfileNode(*elem) profileOrigin         { return profileOrigin{} }
func (*fn) recordProfileTrap(int)                        {}
func (*fn) profileTrapOrigin(int) (uint32, uint32, bool) { return 0, 0, false }

func (*fn) rewindProfileEmission(int)                         {}
func (*fn) profileEmissionRanges() []shared.NativeSourceRange { return nil }

func (*fn) switchProfileOrigin(profileOrigin) {}

func (*fn) enterProfileInline() uint32                      { return 0 }
func (*fn) leaveProfileInline(uint32)                       {}
func (*fn) profileTrapCaller(int) uint32                    { return 0 }
func (*fn) profileInlineFrames() []shared.NativeInlineFrame { return nil }

type profileInlineAddState struct{}

func (*fn) enterProfileInlineAdd(*inlineTarget) profileInlineAddState { return profileInlineAddState{} }
func (*fn) leaveProfileInlineAdd(profileInlineAddState)               {}

func (*fn) enterProfileInstruction() profileOrigin { return profileOrigin{} }
