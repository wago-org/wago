//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"reflect"
	"testing"
)

func remainderPairLatchFixture() *fn {
	f := gpClassEmitter()
	f.policy = currentCodegenPolicy()
	f.hasLoop = true
	f.nLocals = 2
	f.localType = []machineType{mtI32, mtI32}
	f.locals = []localDef{{reg: X19, state: lsReg}, {reg: X20, state: lsReg}}
	f.pinnedLocalMask = maskOf(X19, X20)
	f.ctrl = []ctrlFrame{{kind: cfBlock}, {kind: cfLoop, controlSite: 4}}
	for _, w := range []uint32{nopWord, 0xaa1303e9, 0xaa1403f3, 0x34000013, 0x1ad3092a, 0x1b13a554, 0x7100029f} {
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, w)
	}
	f.sc.trapSites[trapDivZero] = []trapSite{{branch: 12, function: 7, pc: 113}}
	return f
}

func TestRemainderPairLatchProofAndTrapIdentity(t *testing.T) {
	if profileEnabled {
		t.Skip("source profiling preserves original code")
	}
	old, oldForward := remainderPairLatchEnabled, remainderForwardExitEnabled
	remainderPairLatchEnabled, remainderForwardExitEnabled = true, true
	t.Cleanup(func() { remainderPairLatchEnabled = old; remainderForwardExitEnabled = oldForward })
	f := remainderPairLatchFixture()
	after := wasm.ReaderFrom([]byte{0x0b})
	if !f.pairRemainderLatch(&f.ctrl[1], X20, 24, &after) {
		t.Fatal("proved remainder recurrence rejected")
	}
	if len(f.a.B) != 44 || f.ctrl[1].controlSite != 8 || f.phasePadWords != 13 || after.Offset() != 0 {
		t.Fatalf("unexpected layout code=%x loop=%d padding=%d reader=%d", f.a.B, f.ctrl[1].controlSite, f.phasePadWords, after.Offset())
	}
	for _, edge := range [][2]int{{16, 36}, {28, 8}, {32, 44}} {
		if target, ok := branchTarget(edge[0], rdWord(f.a.B, edge[0])); !ok || target != edge[1] {
			t.Fatalf("edge at%d target%d valid%v want%d", edge[0], target, ok, edge[1])
		}
	}
	if got := f.sc.trapSites[trapDivZero]; len(got) != 1 || got[0] != (trapSite{branch: 4, function: 7, pc: 113}) {
		t.Fatalf("trap identity changed: %+v", got)
	}
}

func TestRemainderPairLatchRejectsWithoutMutation(t *testing.T) {
	if profileEnabled {
		t.Skip("source profiling preserves original code")
	}
	old, oldForward := remainderPairLatchEnabled, remainderForwardExitEnabled
	remainderPairLatchEnabled, remainderForwardExitEnabled = true, true
	t.Cleanup(func() { remainderPairLatchEnabled = old; remainderForwardExitEnabled = oldForward })
	cases := []struct {
		name   string
		mutate func(*fn)
	}{
		{"unknown body", func(f *fn) { wrWord(f.a.B, 16, 0x94000000) }},
		{"wide divide", func(f *fn) { wrWord(f.a.B, 16, 0x9ad3092a) }},
		{"changed latch", func(f *fn) { wrWord(f.a.B, 24, 0x7100027f) }},
		{"guard already patched", func(f *fn) { wrWord(f.a.B, 12, 0x34000033) }},
		{"no trap owner", func(f *fn) { f.sc.trapSites[trapDivZero] = nil }},
		{"duplicate trap", func(f *fn) {
			f.sc.trapSites[trapDivZero] = append(f.sc.trapSites[trapDivZero], f.sc.trapSites[trapDivZero][0])
		}},
		{"wrong trap", func(f *fn) {
			f.sc.trapSites[trapMemOOB] = f.sc.trapSites[trapDivZero]
			f.sc.trapSites[trapDivZero] = nil
		}},
		{"relocation", func(f *fn) { f.relocs = []callReloc{{at: 12}} }},
		{"return link", func(f *fn) { f.sc.retSiteHead = 4 }},
		{"pending end", func(f *fn) { f.appendFrameEnd(&f.ctrl[0], 12, true) }},
		{"cold edge", func(f *fn) { f.ensureCtrlMerge(&f.ctrl[0]).coldEdges = []coldEdge{{site: 12}} }},
		{"metadata budget", func(f *fn) { f.sc.trapSites[trapUnreachable] = make([]trapSite, scalarLoopMetadataLimit+1) }},
		{"interruptible", func(f *fn) { f.interruptible = true }},
		{"exceptions", func(f *fn) { f.moduleEH = true }},
		{"calls", func(f *fn) { f.makesCalls = true }},
		{"preserved caller", func(f *fn) { f.preserveCallerPins = true }},
		{"compact", func(f *fn) { f.policy.CompactNative = true }},
		{"reserved branches", func(f *fn) { f.a.ReserveBranch19 = true }},
		{"opaque fragments", func(f *fn) { f.opaqueFragments = true }},
		{"inline locals", func(f *fn) { f.localBase = 1 }},
		{"wide local table", func(f *fn) { f.nLocals = 65 }},
		{"nonzero height", func(f *fn) { f.ctrl[1].height = 1 }},
		{"loop parameters", func(f *fn) { f.ctrl[1].paramN = 1 }},
		{"loop results", func(f *fn) { f.ctrl[1].resultN = 1 }},
		{"pinned scratch", func(f *fn) { f.pinned = f.pinned.add(X9) }},
		{"reserved quotient", func(f *fn) { f.reserved = f.reserved.add(X10) }},
		{"live quotient", func(f *fn) { f.regUser[X10] = new(elem) }},
		{"wrong local type", func(f *fn) { f.localType[0] = mtI64 }},
		{"duplicate home", func(f *fn) {
			f.nLocals = 3
			f.localType = append(f.localType, mtI32)
			f.locals = append(f.locals, localDef{reg: X19, state: lsReg})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := remainderPairLatchFixture()
			tc.mutate(f)
			code := append([]byte(nil), f.a.B...)
			traps := f.sc.trapSites
			for i := range traps {
				traps[i] = append([]trapSite(nil), traps[i]...)
			}
			control := append([]ctrlFrame(nil), f.ctrl...)
			padding := f.phasePadWords
			after := wasm.ReaderFrom([]byte{0x0b})
			if f.pairRemainderLatch(&f.ctrl[1], X20, 24, &after) || !bytes.Equal(code, f.a.B) || !reflect.DeepEqual(traps, f.sc.trapSites) || !reflect.DeepEqual(control, f.ctrl) || f.phasePadWords != padding || after.Offset() != 0 {
				t.Fatal("failed proof changed code, trap ownership or control state")
			}
		})
	}
	f := remainderPairLatchFixture()
	after := wasm.ReaderFrom([]byte{0x01, 0x0b})
	if f.pairRemainderLatch(&f.ctrl[1], X20, 24, &after) {
		t.Fatal("nonterminal latch admitted")
	}
}

func TestRemainderPairLatchNativeOracle(t *testing.T) {
	old, oldForward, oldZero := remainderPairLatchEnabled, remainderForwardExitEnabled, normalZeroBranchEnabled
	remainderForwardExitEnabled, normalZeroBranchEnabled = true, true
	t.Cleanup(func() {
		remainderPairLatchEnabled = old
		remainderForwardExitEnabled = oldForward
		normalZeroBranchEnabled = oldZero
	})
	pairs := make([][2]uint32, 0, 65700)
	for a := uint32(0); a < 256; a++ {
		for b := uint32(0); b < 256; b++ {
			pairs = append(pairs, [2]uint32{a, b})
		}
	}
	pairs = append(pairs, [2]uint32{0xffffffff, 0}, [2]uint32{0, 0xffffffff}, [2]uint32{0xffffffff, 0xffffffff}, [2]uint32{0xffffffff, 0xfffffffe}, [2]uint32{2971215073, 1836311903})
	state := uint32(0x91e10da5)
	next := func() uint32 { state ^= state << 13; state ^= state >> 17; state ^= state << 5; return state }
	for range 64 {
		pairs = append(pairs, [2]uint32{next(), next()})
	}
	for _, guarded := range []bool{false, true} {
		body := []byte{0, 0x3f, 0, 0x1a}
		if guarded {
			body = append(body, 0x02, 0x40, 0x20, 1, 0x45, 0x0d, 0)
		}
		body = append(body, 0x03, 0x40, 0x20, 0, 0x20, 1, 0x22, 0, 0x70, 0x22, 1, 0x0d, 0, 0x0b)
		if guarded {
			body = append(body, 0x0b)
		}
		// Pack both canonical local homes, so an incorrect odd-exit restoration
		// cannot hide behind a correct GCD result in A alone.
		body = append(body, 0x20, 0, 0xad, 0x42, 32, 0x86, 0x20, 1, 0xad, 0x84, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, body)
		var frame rt.TrapFrame
		haveFrame := false
		for _, cfg := range []struct {
			name                           string
			enabled, compact, unroll, fold bool
		}{
			{"disabled", false, false, true, true}, {"paired", true, false, true, true}, {"compact", true, true, true, true}, {"unroll off", true, false, false, true}, {"fold off", true, false, true, false},
		} {
			t.Run(cfg.name+map[bool]string{false: "/raw", true: "/guarded"}[guarded], func(t *testing.T) {
				remainderPairLatchEnabled = cfg.enabled
				var stats ModuleStats
				opts := CompileOptions{CompactNative: cfg.compact, Optimizations: map[string]bool{"scalar-loop-unroll": cfg.unroll, "branch-fold": cfg.fold}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				defer cm.CodeImage.Close()
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["remainder-pair-latch"] != 0) != (cfg.enabled && !cfg.compact && cfg.unroll && cfg.fold && !profileEnabled) {
					t.Fatalf("unexpected admission: %v", stats.Funcs[0].Peephole)
				}
				eng, err := rt.NewEngine()
				if err != nil {
					t.Fatal(err)
				}
				defer eng.Close()
				jm, err := rt.NewJobMemory(65536)
				if err != nil {
					t.Fatal(err)
				}
				defer jm.Close()
				ar, err := rt.NewArena(4096)
				if err != nil {
					t.Fatal(err)
				}
				defer ar.Close()
				code, entry, err := rt.MapCode(cm.Code)
				if err != nil {
					t.Fatal(err)
				}
				defer rt.Unmap(code)
				args, result, trapBuffer := ar.Alloc(16), ar.Alloc(16), ar.Alloc(rt.TrapBufferBytes)
				for _, pair := range pairs {
					clear(result)
					clear(trapBuffer)
					binary.LittleEndian.PutUint64(args, uint64(pair[0])|0xa5a5000000000000)
					binary.LittleEndian.PutUint64(args[8:], uint64(pair[1])|0xdead000000000000)
					err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trapBuffer, result)
					if !guarded && pair[1] == 0 {
						var trap *rt.TrapError
						if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero || len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter {
							t.Fatalf("pair=%x missing precise trap: %v", pair, err)
						}
						if !haveFrame {
							frame = trap.Frames[0]
							haveFrame = true
						} else if trap.Frames[0] != frame {
							t.Fatalf("pair=%x trap=%+v want=%+v", pair, trap.Frames[0], frame)
						}
						continue
					}
					a, b := pair[0], pair[1]
					for b != 0 {
						a, b = b, a%b
					}
					want := uint64(a)<<32 | uint64(b)
					got := binary.LittleEndian.Uint64(result)
					if err != nil || got != want {
						t.Fatalf("pair=%x got=%x want=%x err=%v", pair, got, want, err)
					}
				}
			})
		}
	}
}
