//go:build wago_profile

package wago

import "github.com/wago-org/wago/internal/jitprofile"

type codeProfileSession = jitprofile.Session
type codeProfileToken = jitprofile.SpanToken

func newCodeProfile(options CodeProfileOptions) *CodeProfile { return jitprofile.New(options) }
