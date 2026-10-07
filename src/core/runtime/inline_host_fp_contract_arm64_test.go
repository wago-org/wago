//go:build wago_inline_host_experiment && (linux || darwin) && arm64 && !tinygo

package runtime

import (
	"encoding/binary"
	goruntime "runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func inlineHostProbeClobberFP()

func inlineProbeGrow(depth int) uint64 {
	var buffer [2048]byte
	buffer[0] = byte(depth)
	if depth == 0 {
		goruntime.GC()
		return 0
	}
	value := inlineProbeGrow(depth-1) + uint64(buffer[0])
	goruntime.KeepAlive(&buffer)
	return value
}

// A bounded native segment can have callee-saved FP values that are not Wasm
// local pins. Bounded-work admission alone must never omit their preservation.
func TestInlineBoundedBridgePreservesNativeFP(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	memory, err := mmapRW(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(memory)
	linMem := slicePtr(memory) + 512
	ctrl := memory[1536 : 1536+ctrlFrameSize]
	storeOffHeapU64(linMem-offCustomCtx, uint64(slicePtr(ctrl)))
	var a a64.Asm
	a.StpPre(a64.X19, a64.LR, a64.SP, -16)
	a.MovReg64(a64.X19, a64.X3)
	a.MovReg64(a64.X26, a64.X1)
	var want [8]uint64
	for i := range want {
		want[i] = 0x3ff0000000000000 + uint64(i)*0x111111
		a.MovImm64(a64.X9, want[i])
		a.FmovFromGpr(a64.Reg(8+i), a64.X9, true)
	}
	a.Ldur64(a64.X11, a64.X26, -int32(offCustomCtx))
	a.MovImm64(a64.X9, uint64(1|1<<16)<<32)
	mustEncode(a.Store64(a64.X9, a64.X11, hcImportIdx))
	a.MovImm64(a64.X9, 41)
	mustEncode(a.Store64(a64.X9, a64.X11, hcArgs))
	mustEncode(a.Load64(a64.X16, a64.X11, hcTrampoline))
	a.Blr(a64.X16)
	for i := range want {
		a.FStoreDisp(a64.X19, int32(i*8), a64.Reg(8+i), true)
	}
	a.LdpPost(a64.X19, a64.LR, a64.SP, 16)
	a.Ret()
	code, err := mmapExec(a.B)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(code)
	for _, name := range []string{"scalar", "view", "context", "view-context"} {
		t.Run(name, func(t *testing.T) {
			for n := 0; n < 3; n++ {
				results, trap := memory[1024:1088], memory[1152:1152+TrapBufferBytes]
				clear(results)
				clear(trap)
				calls := 0
				step := func() {
					calls++
					for i, w := range want {
						if got := binary.LittleEndian.Uint64(ctrl[hcSavedV8+i*8:]); got != w {
							t.Fatalf("parked V%d got %#x want %#x", i+8, got, w)
						}
					}
					if inlineProbeGrow(16) != 136 {
						t.Fatal("stack growth")
					}
					goruntime.GC()
					inlineHostProbeClobberFP()
				}
				if name == "view" {
					err = e.CallWithHostBaseFixedViewBoundedLive(runtimebridge.GrantHostScalarCall(), slicePtr(code), nil, linMem, trap, results, ctrl, 1|1<<16, func(args, out []uint64) {
						if args[0] != 41 {
							t.Fatal(args)
						}
						step()
						out[0] = 42
					})

				} else if name == "view-context" {
					context := new(byte)
					callback := func(args, out []uint64) {
						if args[0] != 41 {
							t.Fatal(args)
						}
						step()
						out[0] = 42
					}
					err = e.CallWithHostBaseFixedViewBoundedContextLive(runtimebridge.GrantHostScalarCall(), slicePtr(code), nil, linMem, trap, results, ctrl, 1|1<<16, unsafe.Pointer(context), func(pointer unsafe.Pointer, args, out []uint64) {
						if pointer != unsafe.Pointer(context) {
							t.Fatal("context changed")
						}
						callback(args, out)
					}, callback)
				} else if name == "context" {
					context := new(byte)
					callback := func(a0, a1 uint64) uint64 {
						if a0 != 41 {
							t.Fatal(a0)
						}
						step()
						return 42
					}
					err = e.CallWithHostBaseScalarBoundedContextLive(runtimebridge.GrantHostScalarCall(), slicePtr(code), nil, linMem, trap, results, ctrl, 1|1<<16, unsafe.Pointer(context), func(pointer unsafe.Pointer, a0, a1 uint64) uint64 {
						if pointer != unsafe.Pointer(context) {
							t.Fatal("context changed")
						}
						return callback(a0, a1)
					}, callback)
				} else {
					err = e.CallWithHostBaseScalarBoundedLive(runtimebridge.GrantHostScalarCall(), slicePtr(code), nil, linMem, trap, results, ctrl, 1|1<<16, func(a0, a1 uint64) uint64 {
						if a0 != 41 {
							t.Fatal(a0)
						}
						step()
						return 42
					})
				}
				if err != nil || calls != 1 {
					t.Fatalf("calls=%d, err=%v", calls, err)
				}
				if got := binary.LittleEndian.Uint64(ctrl[hcResults:]); got != 42 {
					t.Fatalf("host result = %d", got)
				}
				for i, w := range want {
					if got := binary.LittleEndian.Uint64(results[i*8:]); got != w {
						t.Fatalf("V%d lost: got %#x want %#x", i+8, got, w)
					}
				}
				goruntime.KeepAlive(code)
				goruntime.KeepAlive(memory)
				goruntime.KeepAlive(ctrl)
			}
		})
	}
}
