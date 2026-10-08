//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"testing"
)

func TestTailInvocationFactoryRejectsBeforePublication(t *testing.T) {
	ar, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	caller, target := ar.Alloc(abi.RV64InstanceContextBytes), ar.Alloc(abi.RV64InstanceContextBytes)
	valid := NativeTailSite{CallerHome: 0x1000, CallerContext: caller, TargetContext: target, Target: NativeImportTarget{Entry: 0x2000, Home: 0x3000, Context: tailRAPointer(target), HostTrampoline: 0x4000, StackOwnedActivation: true, InvocationContext: true}, FrameBytes: 64, SourceBase: 48, SourceStride: 8, Parameters: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I64}, Continuations: []NativeTailCodeRange{{0x5000, 64}}}
	if _, err := nativeTailDispatcherBound([]NativeTailSite{valid}, 64); err != nil {
		t.Fatal("retained descriptor control", err)
	}
	for _, tc := range []struct {
		name   string
		modify func(*NativeTailSite)
	}{
		{"old-caller-context", func(s *NativeTailSite) { s.CallerContext = s.CallerContext[:128] }},
		{"old-target-context", func(s *NativeTailSite) { s.TargetContext = s.TargetContext[:128] }},
		{"context-pointer", func(s *NativeTailSite) { s.Target.Context += 8 }},
		{"legacy-target", func(s *NativeTailSite) { s.Target.InvocationContext = false }},
		{"unframed-target", func(s *NativeTailSite) { s.Target.StackOwnedActivation = false }},
		{"argument-homes", func(s *NativeTailSite) { s.SourceBase = 64 }},
		{"pair-in-scalar-home", func(s *NativeTailSite) { s.Parameters = []wasm.ValType{wasm.V128} }},
		{"missing-continuation-owner", func(s *NativeTailSite) { s.Continuations = nil }},
		{"wrapped-continuation", func(s *NativeTailSite) { s.Continuations = []NativeTailCodeRange{{^uintptr(0) &^ 3, 8}} }},
		{"overwide-signature", func(s *NativeTailSite) { s.Parameters = make([]wasm.ValType, 257) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			tc.modify(&s)
			owner, err := NewNativeTailDispatcher([]NativeTailSite{s}, 64)
			if owner != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid factory allocated", err)
			}
			if binary.LittleEndian.Uint64(caller[abi.RV64TailCapabilityOffset:]) != 0 {
				t.Fatal("failed factory bound context")
			}
		})
	}
	if owner, err := NewNativeTailDispatcher([]NativeTailSite{valid}, MaxCodeBytes); owner != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("combined budget reached allocation", err)
	}
	binary.LittleEndian.PutUint64(caller[abi.RV64TailCapabilityOffset:], 1)
	if owner, err := NewNativeTailDispatcher([]NativeTailSite{valid}, 64); owner != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("occupied context reached allocation", err)
	}
}
