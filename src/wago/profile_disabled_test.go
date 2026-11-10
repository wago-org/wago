//go:build !wago_profile

package wago

import (
	"strings"
	"testing"
	"unsafe"
)

func TestCodeProfileRequiresBuildTag(t *testing.T) {
	session := NewCodeProfile(CodeProfileOptions{})
	defer session.Close()
	err := NewRuntimeConfig().WithCodeProfile(session).Validate()
	if err == nil || !strings.Contains(err.Error(), "wago_profile") {
		t.Fatalf("expected explicit unavailable-feature error, got %v", err)
	}
}

func TestCodeProfileCacheStateIsAbsent(t *testing.T) {
	if got := unsafe.Sizeof(profileCacheState{}); got != 0 {
		t.Fatalf("ordinary compiled module carries %d bytes of profiling state", got)
	}
}

func TestCodeProfileExecutionStateIsAbsent(t *testing.T) {
	for _, state := range []struct {
		name string
		size uintptr
	}{
		{"public session", unsafe.Sizeof(CodeProfile{})},
		{"public span token", unsafe.Sizeof(CodeProfileSpanToken{})},
		{"instance", unsafe.Sizeof(profileInstanceState{})},
		{"invocation", unsafe.Sizeof(profileInvocationState{})},
		{"instantiation", unsafe.Sizeof(profileBuilderState{})},
	} {
		t.Run(state.name, func(t *testing.T) {
			if state.size != 0 {
				t.Fatalf("ordinary %s carries %d bytes of profiling state", state.name, state.size)
			}
		})
	}
}
