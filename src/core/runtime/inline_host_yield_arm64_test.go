//go:build (linux || darwin) && arm64 && !tinygo

package runtime

import (
	"encoding/binary"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"unsafe"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

var inlineYieldGCComplete atomic.Uint64
var inlineYieldCallbackCount atomic.Uint64

//go:nosplit
func inlineYieldProgress() uint64 {
	inlineYieldCallbackCount.Add(1)
	return inlineYieldGCComplete.Load()
}

// These callbacks deliberately provide no stack-growth check or explicit yield.
// The live bridge must allow the collector to finish before the guest returns.
//
//go:nosplit
func inlineYieldScalar(uint64, uint64) uint64 { return inlineYieldProgress() }

//go:nosplit
func inlineYieldContext(unsafe.Pointer, uint64, uint64) uint64 { return inlineYieldProgress() }

//go:nosplit
func inlineYieldView(_ []uint64, results []uint64) { results[0] = inlineYieldProgress() }

//go:nosplit
func inlineYieldViewContext(_ unsafe.Pointer, _ []uint64, results []uint64) {
	results[0] = inlineYieldProgress()
}

//go:nosplit
func inlineYieldI32(int32) int32 { return int32(inlineYieldProgress()) }

func TestInlineHostBridgeAllowsGCWithLeafCallbacks(t *testing.T) {
	old := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(old)
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	memory, err := mmapRW(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(memory)
	linMem := slicePtr(memory) + 512
	results, trap := memory[1024:1088], memory[1152:1152+TrapBufferBytes]
	ctrl := memory[1536 : 1536+ctrlFrameSize]
	storeOffHeapU64(linMem-offCustomCtx, uint64(slicePtr(ctrl)))
	storeOffHeapU64(linMem-abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	const slots uint32 = 1 | 1<<16
	var a a64.Asm
	a.StpPre(a64.X19, a64.LR, a64.SP, -16)
	a.StpPre(a64.X20, a64.X21, a64.SP, -16)
	a.MovReg64(a64.X19, a64.X3)
	a.MovReg64(a64.X26, a64.X1)
	// Stop when a callback observes collection completion. The hard cap makes
	// a broken non-yielding bridge fail without an unbounded native loop; it
	// is not a requirement that the collector finish in a fixed small batch.
	const maxCallbacks = 1 << 24
	a.MovImm64(a64.X20, maxCallbacks)
	loop := a.Len()
	a.Ldur64(a64.X11, a64.X26, -int32(offCustomCtx))
	a.MovImm64(a64.X9, uint64(slots)<<32)
	mustEncode(a.Store64(a64.X9, a64.X11, hcImportIdx))
	mustEncode(a.Load64(a64.X16, a64.X11, hcTrampoline))
	a.Blr(a64.X16)
	a.Ldur64(a64.X11, a64.X26, -int32(offCustomCtx))
	mustEncode(a.Load64(a64.X9, a64.X11, hcResults))
	completed := a.Cbnz64(a64.X9)
	a.SubImm64(a64.X20, a64.X20, 1)
	branch := a.Cbnz64(a64.X20)
	if !a.PatchBranch19(branch, loop) {
		t.Fatal("callback loop branch is out of range")
	}
	if !a.PatchBranch19(completed, a.Len()) {
		t.Fatal("collection completion branch is out of range")
	}
	a.Ldur64(a64.X11, a64.X26, -int32(offCustomCtx))
	mustEncode(a.Load64(a64.X9, a64.X11, hcResults))
	mustEncode(a.Store64(a64.X9, a64.X19, 0))
	a.LdpPost(a64.X20, a64.X21, a64.SP, 16)
	a.LdpPost(a64.X19, a64.LR, a64.SP, 16)
	a.Ret()
	code, err := mmapExec(a.B)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(code)
	for _, kind := range []string{"scalar", "view", "context", "view-context", "integer-context", "integer-view-context", "integer-i32", "multi-view"} {
		for _, cancel := range []bool{false, true} {
			name := kind
			if cancel {
				name += "/cancel"
			}
			t.Run(name, func(t *testing.T) {
				clear(results)
				clear(trap)
				bridge := inlineHostBridgeAddr()
				switch kind {
				case "view", "view-context", "multi-view":
					bridge = inlineHostViewBridgeAddr()
				case "integer-context", "integer-i32":
					bridge = inlineHostIntegerBridgeAddr()
				case "integer-view-context":
					bridge = inlineHostIntegerViewBridgeAddr()
				}
				binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], uint64(bridge))
				inlineYieldGCComplete.Store(0)
				inlineYieldCallbackCount.Store(0)
				collected := make(chan struct{})
				nativeReturned := make(chan struct{})
				go func() {
					defer close(collected)
					// Start collection only after this owner has entered native
					// code. A pre-entry collection must not satisfy the test.
					for inlineYieldCallbackCount.Load() == 0 {
						select {
						case <-nativeReturned:
							return
						default:
							goruntime.Gosched()
						}
					}
					goruntime.GC()
					if cancel {
						atomic.StoreUint32((*uint32)(unsafe.Pointer(&trap[0])), uint32(TrapInterrupted))
					}
					inlineYieldGCComplete.Store(1)
				}()
				defer func() {
					close(nativeReturned)
					<-collected
				}()
				var outcome uint64
				switch kind {
				case "scalar":
					outcome = inlineHostEnter(inlineYieldScalar, slicePtr(code), 0, linMem, slicePtr(trap), slicePtr(results), engine.stackTop, slicePtr(ctrl), slots)
				case "view":
					outcome = inlineHostViewEnter(inlineYieldView, slicePtr(code), 0, linMem, slicePtr(trap), slicePtr(results), engine.stackTop, slicePtr(ctrl), slots)
				case "context":
					outcome = inlineHostContextEnter(inlineYieldContext, unsafe.Pointer(&inlineYieldGCComplete), slicePtr(code), 0, linMem, slicePtr(trap), slicePtr(results), engine.stackTop, slicePtr(ctrl), slots)
				case "view-context":
					outcome = inlineHostViewContextEnter(inlineYieldViewContext, unsafe.Pointer(&inlineYieldGCComplete), slicePtr(code), 0, linMem, slicePtr(trap), slicePtr(results), engine.stackTop, slicePtr(ctrl), slots)
				case "integer-context":
					outcome = inlineHostIntegerContextEnter(inlineYieldContext, unsafe.Pointer(&inlineYieldGCComplete), slicePtr(code), 0, linMem, slicePtr(trap), slicePtr(results), engine.stackTop, slicePtr(ctrl), slots)
				case "integer-view-context":
					outcome = inlineHostIntegerViewContextEnter(inlineYieldViewContext, unsafe.Pointer(&inlineYieldGCComplete), slicePtr(code), 0, linMem, slicePtr(trap), slicePtr(results), engine.stackTop, slicePtr(ctrl), slots)
				case "integer-i32":
					outcome = inlineHostIntegerI32Enter(inlineYieldI32, slicePtr(code), 0, linMem, slicePtr(trap), slicePtr(results), engine.stackTop, slicePtr(ctrl), slots)
				case "multi-view":
					outcome = inlineHostMultiViewEnter(inlineYieldView, slicePtr(code), 0, linMem, slicePtr(trap), slicePtr(results), engine.stackTop, slicePtr(ctrl), []uint32{slots})
				}
				if cancel {
					if outcome != 2 || TrapCode(loadTrap(trap)) != TrapInterrupted {
						t.Fatalf("cancelled bridge outcome=%d trap=%d callbacks=%d, want interrupted Go return", outcome, loadTrap(trap), inlineYieldCallbackCount.Load())
					}
					return
				}
				if outcome != 0 || loadTrap(trap) != 0 {
					t.Fatalf("bridge outcome=%d trap=%d", outcome, loadTrap(trap))
				}
				if got := binary.LittleEndian.Uint64(results); got != 1 {
					t.Fatalf("collector progress observed by last callback = %d after %d callbacks (limit %d), want 1 before native return", got, inlineYieldCallbackCount.Load(), maxCallbacks)
				}
			})
		}
	}
}
