//go:build (linux || darwin) && arm64 && !tinygo

package arm64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func variantRouteModule(pair, trap bool) []byte {
	params := []wasm.ValType{wasm.I32}
	ownType := uint32(0)
	if pair {
		params = append(params, wasm.I32)
		ownType = 1
	}
	types := [][]byte{wasmtest.FuncType(params, []wasm.ValType{wasm.I32})}
	if pair {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("step")...), 0, 0)
	body := []byte{1, 2, 0x7f, 0x03, 0x40, 0x20, 2, 0x20, 1}
	if pair {
		body = append(body, 0x41, 7)
	}
	body = append(body, 0x10, 0, 0x20, 1, 0x41, 3, 0x6a)
	if pair {
		body = append(body, 0x41, 5)
	}
	body = append(body, 0x10, 1, 0x6a, 0x6a, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	if trap {
		body = append(body[:len(body)-1], 0x41, 0, 0x6e, 0x0b)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imp, imp)), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(ownType))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

// Public imported callers now require unwind maps. Exercise specialization at
// its actual backend boundary: no candidate plan, scalar aliases, bounded loop.
// Candidate maps remain an independently checked exclusion, even when rootless.
func TestNativeImportVariantRoutesARM64(t *testing.T) {
	if profileEnabled {
		t.Skip("profile retains the ordinary body")
	}
	t.Setenv("WAGO_ARM64_NATIVE_IMPORT_TARGET_CACHE", "1")
	// Exercise both compaction choices even under an ambient WAGO_COMPACT.
	enabled, disabled := nativeCompactionEnabled, nativeCompactionDisabled
	nativeCompactionEnabled, nativeCompactionDisabled = false, false
	t.Cleanup(func() { nativeCompactionEnabled, nativeCompactionDisabled = enabled, disabled })
	for _, compact := range []bool{false, true} {
		for _, pair := range []bool{false, true} {
			for _, trap := range []bool{false, true} {
				t.Run(fmt.Sprintf("compact=%t/pair=%t/trap=%t", compact, pair, trap), func(t *testing.T) {
					m, err := wasm.DecodeModule(variantRouteModule(pair, trap))
					if err != nil {
						t.Fatal(err)
					}
					if err = wasm.ValidateModule(m); err != nil {
						t.Fatal(err)
					}
					for _, candidate := range []bool{false, true} {
						var images [2][]byte
						for mode, flag := range []string{"0", "1"} {
							t.Setenv("WAGO_ARM64_NATIVE_IMPORT_VARIANT", flag)
							var plans *shared.GCModuleFrameRootPlan
							if candidate {
								plans = shared.NewGCModuleFrameRootPlan(1)
								if !plans.MarkFunction(0) || !plans.ReserveFunctions(1) {
									t.Fatal("plans")
								}
								p, ok := plans.BeginFunction(0)
								if !ok {
									t.Fatal("plan")
								}
								p.Candidate, p.Exact, p.UnwindOnly = true, true, true
								if !p.SetLiveMasks([]uint64{0, 0}, 0, 2) {
									t.Fatal("masks")
								}
							}
							cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, CompactNative: compact, SyncHostCalls: true, GCFrameRoots: plans, ImportBindings: []ImportBinding{{Dynamic: true, ImportIndex: 0}, {Dynamic: true, ImportIndex: 1}}})
							if err != nil {
								t.Fatal(err)
							}
							if cm.CodeImage != nil {
								defer cm.CodeImage.Close()
							}
							images[mode] = append([]byte(nil), cm.Code...)

							if candidate {
								if p := plans.Function(0); !p.Exact || p.CallsiteCount() != 4 {
									t.Fatal("candidate lost exact return maps")
								}
							} else {
								if mode == 1 {
									// The guard must land on the frame-record save, not the next
									// instruction. This catches a stale +4 displacement before execution.
									router := cm.InternalEntry[0] + 12
									word := binary.LittleEndian.Uint32(cm.Code[router:])
									if word&0xff00001f != 0xb5000011 {
										t.Fatalf("entry guard = %#x", word)
									}
									delta := int32(word<<8) >> 13
									target := router + int(delta)*4
									if target < 0 || target+4 > len(cm.Code) || binary.LittleEndian.Uint32(cm.Code[target:]) != 0xa9bf7bfd {
										t.Fatalf("native guard target %#x does not start with STP FP,LR", target)
									}
								}
								executeVariantRoute(t, cm.Code, cm.Entry[0], pair, trap)
							}
						}
						if candidate {
							if !bytes.Equal(images[0], images[1]) {
								t.Fatal("candidate plan admitted variant")
							}
						} else if bytes.Equal(images[0], images[1]) || len(images[1]) <= len(images[0]) {
							t.Fatal("eligible selector did not add second body")
						}
					}
				})
			}
		}
	}
}

func executeVariantRoute(t *testing.T, code []byte, offset int, pair, wantTrap bool) {
	t.Helper()
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
	mapped, entry, err := runtime.MapCode(code)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(mapped)
	// Both wrapper and register entries obey the native scalar dispatch ABI.
	var a a64.Asm
	a.MovReg64(X4, X3)
	if pair {
		if !a.Load32(X1, X0, 8) {
			t.Fatal("leaf arg")
		}
	}
	if !a.Load32(X0, X0, 0) {
		t.Fatal("leaf arg")
	}
	bodyStart := a.Len()
	if pair {
		a.Mul32(X2, X0, X1)
		a.Add32(X0, X2, X0)
		a.Sub32(X0, X0, X1)
	} else {
		a.AddImm32(X0, X0, 1)
	}
	body := append([]byte(nil), a.B[bodyStart:]...)
	if !a.Store64(X0, X4, 0) {
		t.Fatal("leaf result")
	}
	a.Ret()
	for a.Len() < runtime.NativeScalarLeafRegisterEntryOffset {
		a.Nop()
	}
	a.B = append(a.B, body...)
	a.Ret()
	leaf, leafEntry, err := runtime.MapCode(a.B)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(leaf)
	arity := 1
	if pair {
		arity = 2
	}
	thunk, thunkEntry, err := runtime.MapCode(HostIndirectSyncThunk(0, arity, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(thunk)
	args, out := arena.Alloc(8), arena.Alloc(8)
	trap, ctrl := arena.Alloc(runtime.TrapBufferBytes), arena.Alloc(runtime.HostCtrlFrameBytes)
	ptr := func(b []byte) uintptr { return uintptr(unsafe.Pointer(&b[0])) }
	jm.SetCustomCtx(ptr(ctrl))
	dispatch := arena.Alloc(2 * runtime.ImportDispatchEntryBytes)
	jm.SetImportDispatchPtr(ptr(dispatch))
	ctx := arena.Alloc(runtime.InstanceContextBytes)
	jm.CaptureInstanceContextBytes(ctx)
	for _, native := range []bool{false, true} {
		t.Logf("native=%t", native)
		tag, target := uint64(ptr(ctx))|runtime.ImportDispatchCallerGoHostTag, uint64(thunkEntry)
		if native {
			tag = uint64(ptr(ctx)) | runtime.ImportDispatchCallerNativeLeafTag
			target = uint64(leafEntry)
		}
		for i := 0; i < 2; i++ {
			d := dispatch[i*runtime.ImportDispatchEntryBytes:]
			binary.LittleEndian.PutUint64(d[runtime.ImportDispatchCallerContextOffset:], tag)
			binary.LittleEndian.PutUint64(d[runtime.ImportDispatchTargetContextOffset:], uint64(ptr(ctx)))
			binary.LittleEndian.PutUint64(d[runtime.ImportDispatchHomeLinMemOffset:], uint64(ptr(jm.LinearMemory())))
			binary.LittleEndian.PutUint64(d[runtime.ImportDispatchCodePtrOffset:], target)
		}
		for _, n := range []uint32{0, 1, 17, 129, 1024, 65536} {
			count := uint64(n)
			if count == 0 {
				count = 1
			}
			calls := 0
			host := func(_ uintptr, index uint32, in, out []uint64) {
				if index > 1 || len(in) != arity || len(out) != 1 {
					t.Fatalf("host ABI index=%d in=%v out=%v", index, in, out)
				}
				calls++
				v := uint32(in[0])
				result := v + 1
				if pair {
					b := uint32(in[1])
					result = v*b + v - b
				}
				out[0] = uint64(result)
			}
			binary.LittleEndian.PutUint64(args, uint64(n))
			err := eng.CallWithHost(entry+uintptr(offset), args, jm.LinearMemory(), trap, out, ctrl, host)
			if wantTrap {
				var tr *runtime.TrapError
				if !errors.As(err, &tr) || tr.Code != runtime.TrapDivZero {
					t.Fatalf("native=%t n=%d trap=%v", native, n, err)
				}
			} else {
				want := uint32(count * (count + 4))
				if pair {
					want = uint32(7*count*count - count)
				}
				if got := binary.LittleEndian.Uint32(out); err != nil || got != want {
					t.Fatalf("native=%t n=%d result=%d err=%v want=%d", native, n, got, err, want)
				}
			}
			wantCalls := 2 * int(count)
			if native {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("native=%t calls=%d want=%d", native, calls, wantCalls)
			}
		}
	}
}
