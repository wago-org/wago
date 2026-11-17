//go:build amd64 && linux && wago_experimental_bounded_vector_polls

package amd64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	goruntime "runtime"
	"sort"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
)

// delayedPairPollCode changes no original offsets or branch targets. The
// selected MOV-pointer/MOV-cell prefix jumps to appended test instrumentation;
// its original TEST and conditional trap branch remain byte-identical.
func delayedPairPollCode(t *testing.T, original []byte, site int, counter []byte, visit int) []byte {
	t.Helper()
	if len(counter) < 8 || visit < 1 || visit > math.MaxInt32 {
		t.Fatal("invalid counter/visit")
	}
	type match struct {
		start, pointerEnd, end int
		scratch                Reg
	}
	var matches []match
	for r := Reg(0); r < 16; r++ {
		if r == RSP || r == RBX {
			continue
		}
		a := &encoderamd64.Asm{}
		a.Load64(r, RBX, -offTrapCellPtr)
		pointerEnd := a.Len()
		a.Load32(r, r, 0)
		end := a.Len()
		a.TestSelf(r, false)
		for at := 0; at < len(original); {
			n := bytes.Index(original[at:], a.B)
			if n < 0 {
				break
			}
			start := at + n
			matches = append(matches, match{start, start + pointerEnd, start + end, r})
			at = start + len(a.B)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].start < matches[j].start })
	if site < 0 || site >= len(matches) {
		t.Fatalf("poll site %d missing: %v", site, matches)
	}
	m := matches[site]
	if m.end-m.start < 5 {
		t.Fatal("insufficient complete MOV instructions for rel32 jump")
	}
	var tmp []Reg
	for _, r := range []Reg{R10, R11, RAX} {
		if r != m.scratch {
			tmp = append(tmp, r)
		}
	}
	addr, value := tmp[0], tmp[1]
	a := &encoderamd64.Asm{}
	a.B = append(a.B, original[m.start:m.pointerEnd]...) // original trap pointer load
	a.B = append(a.B, 0x9c)                              // PUSHFQ; instrumentation must not leak flags
	a.Push(addr)
	a.Push(value)
	a.MovImm64(addr, uint64(uintptr(unsafe.Pointer(&counter[0]))))
	a.Load32(value, addr, 0)
	a.AluRI(0, value, 1, false)
	a.Store32(addr, 0, value)
	a.CmpImm32Mem(addr, 0, int32(visit))
	noTrap := a.JccPlaceholder(condNE)
	a.StoreImm32Mem(m.scratch, 0, int32(runtime.TrapInterrupted))
	a.PatchRel32(noTrap, a.Len())
	a.Pop(value)
	a.Pop(addr)
	a.B = append(a.B, 0x9d)                            // POPFQ; RSP fully restored
	a.B = append(a.B, original[m.pointerEnd:m.end]...) // original cell read, even on activation
	code := append([]byte(nil), original...)
	trampoline := len(code)
	code = append(code, a.B...)
	jump := func(from, to int) {
		delta := int64(to) - int64(from+5)
		if delta < math.MinInt32 || delta > math.MaxInt32 {
			t.Fatal("test trampoline rel32 overflow")
		}
		code[from] = 0xe9
		binary.LittleEndian.PutUint32(code[from+1:], uint32(int32(delta)))
	}
	back := len(code)
	code = append(code, 0, 0, 0, 0, 0)
	jump(back, m.end)
	for i := m.start; i < m.end; i++ {
		code[i] = 0x90
	}
	jump(m.start, trampoline)
	if !bytes.Equal(code[m.end:len(original)], original[m.end:]) || !bytes.Equal(code[:m.start], original[:m.start]) {
		t.Fatal("original code outside complete MOV prefix changed")
	}
	return code
}

func TestGemmPairDelayedPollInterruption(t *testing.T) {
	// This existing fixture qualifies the XMM policy; YMM has separate tests.
	oldWide := regionWideAdjacentEnabled
	regionWideAdjacentEnabled = false
	defer func() { regionWideAdjacentEnabled = oldWide }()
	old, force := regionAdjacentEnabled, regionLoopTestFast
	defer func() { regionAdjacentEnabled, regionLoopTestFast = old, force }()
	regionAdjacentEnabled, regionLoopTestFast = true, false
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		t.Run(fmt.Sprint(features), func(t *testing.T) {
			cm, err := CompileModuleWith(gemmAdjacentPollFixture(t), CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: features})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if len(boundedPollInjectionSites(cm.Code)) != 4 {
				t.Fatal("actual GEMM packed path absent")
			}
			eng, err := runtime.NewEngine()
			if err != nil {
				t.Fatal(err)
			}
			defer eng.Close()
			jm, err := runtime.NewJobMemory(65536)
			if err != nil {
				t.Fatal(err)
			}
			defer jm.Close()
			arena, err := runtime.NewArena(4096)
			if err != nil {
				t.Fatal(err)
			}
			defer arena.Close()
			args, results, trap, counter := arena.Alloc(256), arena.Alloc(256), arena.Alloc(runtime.TrapBufferBytes), arena.Alloc(8)
			normal, entry, err := runtime.MapCode(cm.Code)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Unmap(normal)
			for _, n := range []int{2 * boundedVectorPollGroups, 2*boundedVectorPollGroups + 1, 3 * boundedVectorPollGroups, 35} {
				polls := (n + boundedVectorPollGroups - 1) / boundedVectorPollGroups
				for _, target := range []int{2, polls} {
					t.Run(fmt.Sprintf("n%d-visit%d", n, target), func(t *testing.T) {
						clear(counter)
						clear(trap)
						code := delayedPairPollCode(t, cm.Code, 3, counter, target)
						mapped, patchedEntry, err := runtime.MapCode(code)
						if err != nil {
							t.Fatal(err)
						}
						defer runtime.Unmap(mapped)
						lin := jm.LinearMemory()
						init := func() {
							clear(lin)
							binary.LittleEndian.PutUint64(lin[8192:], math.Float64bits(2))
							for i := 0; i < 2*n; i++ {
								binary.LittleEndian.PutUint64(lin[4096+i*8:], math.Float64bits(float64(i+1)))
								binary.LittleEndian.PutUint64(lin[128+i*8:], math.Float64bits(1))
							}
						}
						init()
						expected := append([]byte(nil), lin...)
						first := n % boundedVectorPollGroups
						if first == 0 {
							first = boundedVectorPollGroups
						}
						completed := first + (target-1)*boundedVectorPollGroups
						for i := 0; i < 2*completed; i++ {
							binary.LittleEndian.PutUint64(expected[128+i*8:], math.Float64bits(float64(i+1)*3+1))
						}
						for i, v := range []uint64{128, 8192, 4096, 0, uint64(n) * 16} {
							binary.LittleEndian.PutUint64(args[i*8:], v)
						}
						err = eng.Call(patchedEntry+uintptr(cm.Entry[0]), args, lin, trap, results)
						goruntime.KeepAlive(counter)
						var te *runtime.TrapError
						if !errors.As(err, &te) || te.Code != runtime.TrapInterrupted {
							t.Fatalf("want delayed interruption: %v", err)
						}
						if binary.LittleEndian.Uint32(counter) != uint32(target) {
							t.Fatalf("wrong poll visit count: %d", binary.LittleEndian.Uint32(counter))
						}
						if len(te.Frames) == 0 || te.Frames[0].FunctionIndex != 0 || te.Frames[0].HasProgramCounter {
							t.Fatalf("wrong trap origin: %+v", te.Frames)
						}
						if !bytes.Equal(lin, expected) {
							t.Fatalf("not exact %d-source-iteration prefix", completed)
						}
						// Same trampoline with unreachable activation proves successful checks,
						// scratch reload, counter flags and balanced stack through normal return.
						clear(counter)
						clear(trap)
						init()
						passCode := delayedPairPollCode(t, cm.Code, 3, counter, polls+1)
						pass, passEntry, err := runtime.MapCode(passCode)
						if err != nil {
							t.Fatal(err)
						}
						defer runtime.Unmap(pass)
						if err = eng.Call(passEntry+uintptr(cm.Entry[0]), args, lin, trap, results); err != nil {
							t.Fatal("successful-poll path failed", err)
						}
						goruntime.KeepAlive(counter)
						if binary.LittleEndian.Uint32(counter) != uint32(polls) || binary.LittleEndian.Uint64(results) != uint64(n*16) {
							t.Fatal("wrong final poll count or local counter")
						}
						got := append([]byte(nil), lin...)
						clear(trap)
						init()
						if err = eng.Call(entry+uintptr(cm.Entry[0]), args, lin, trap, results); err != nil {
							t.Fatal("engine reuse failed", err)
						}
						if !bytes.Equal(got, lin) {
							t.Fatal("instrumentation changed completed memory")
						}
					})
				}
			}
		})
	}
}
