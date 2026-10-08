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

func TestTailInvocationHostFactoryBounds(t *testing.T) {
	ar, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	ctx := ar.Alloc(abi.RV64InstanceContextBytes)
	valid := NativeTailSite{HostEntry: true, CallerHome: 0x1000, CallerContext: ctx, TargetContext: ctx, Target: NativeImportTarget{Entry: 0x2000, Home: 0x1000, Context: tailRAPointer(ctx), HostTrampoline: 0x4000, StackOwnedActivation: true, InvocationContext: true}, FrameBytes: 64, SourceBase: 48, SourceStride: 8, Parameters: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}, Continuations: []NativeTailCodeRange{{0x5000, 64}}}
	if _, err := nativeTailDispatcherBound([]NativeTailSite{valid}, 64); err != nil {
		t.Fatal("retained host wrapper", err)
	}
	other := ar.Alloc(abi.RV64InstanceContextBytes)
	for _, tc := range []struct {
		name   string
		modify func(*NativeTailSite)
	}{
		{"foreign-home", func(s *NativeTailSite) { s.Target.Home += 8 }},
		{"foreign-context", func(s *NativeTailSite) { s.TargetContext = other; s.Target.Context = tailRAPointer(other) }},
		{"vector-params", func(s *NativeTailSite) { s.Parameters = []wasm.ValType{wasm.V128}; s.SourceStride = 16 }},
		{"vector-results", func(s *NativeTailSite) { s.Results = []wasm.ValType{wasm.V128} }},
		{"wide-params", func(s *NativeTailSite) {
			s.Parameters = make([]wasm.ValType, 65)
			for i := range s.Parameters {
				s.Parameters[i] = wasm.I32
			}
			s.FrameBytes = 576
		}},
		{"wide-results", func(s *NativeTailSite) {
			s.Results = make([]wasm.ValType, 65)
			for i := range s.Results {
				s.Results[i] = wasm.I32
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			tc.modify(&s)
			owner, err := NewNativeTailDispatcher([]NativeTailSite{s}, 64)
			if owner != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid host factory allocated", err)
			}
			if binary.LittleEndian.Uint64(ctx[abi.RV64TailCapabilityOffset:]) != 0 {
				t.Fatal("invalid host factory bound")
			}
		})
	}
	p := nativeTailProofSite(valid)
	p.invocationContext = false
	p.stackLimit = 0x10000
	p.stackTop = 0x20000
	p.argumentRanges = []tailRARange{{0x30000, 8}}
	p.resultRanges = []tailRARange{{0x40000, 8}}
	if err := p.validate(); !errors.Is(err, ErrUnsupported) {
		t.Fatal("legacy private host view accepted", err)
	}
}
