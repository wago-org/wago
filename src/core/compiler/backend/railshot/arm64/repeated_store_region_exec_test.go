//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

func TestRepeatedStoreRegionNativePrefix(t *testing.T) {
	group := []uint32{0xf900001f, nopWord, 0xd2800023, 0xf9000403}                          // zero [X0], then store 1 at [X0+8]
	words := append(append(append([]uint32{}, group...), 0xb40000c1, 0xf9400004), group...) // CBZ X1, continuation; LDR X4,[X0]
	words = append(words, 0xd28000e0, 0xd65f03c0)                                           // return 7
	original := make([]byte, len(words)*4)
	for i, w := range words {
		binary.LittleEndian.PutUint32(original[i*4:], w)
	}
	optimized := append([]byte(nil), original...)
	if !repeatStoreRegionAt(optimized, 0, 24, 4, nativeConstantTargets(optimized), nil) {
		t.Fatal("safe repeated stores rejected")
	}
	for _, code := range [][]byte{original, optimized} {
		mapped, err := arm64spike.MapExec(code)
		if err != nil {
			t.Fatal(err)
		}
		for _, takeExit := range []uint64{0, 1} {
			memory := [16]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}
			got := arm64spike.Call2(uintptr(unsafe.Pointer(&mapped[0])), uintptr(unsafe.Pointer(&memory[0])), uintptr(takeExit))
			runtime.KeepAlive(&memory)
			if got != 7 || binary.LittleEndian.Uint64(memory[:8]) != 0 || binary.LittleEndian.Uint64(memory[8:]) != 1 {
				t.Fatalf("exit=%d result=%d memory=%x", takeExit, got, memory)
			}
		}
		coreruntime.Unmap(mapped)
	}
}

func repeatedStoreRegionModule(t testing.TB) *wasm.Module {
	body := []byte{0, 0x03, 0x40} // no locals; loop (void)
	stores := func() {
		for offset := byte(0); offset < 64; offset += 8 {
			body = append(body,
				0x20, 0, // local.get 0
				0x42, 0, // i64.const 0
				0x37, 3, offset, // i64.store align=8 offset
			)
		}
	}
	stores()
	body = append(body, 0x20, 0, 0x28, 2, 0, 0x1a) // intervening load; drop
	stores()
	body = append(body,
		0x41, 0, 0x0d, 0, // false loop backedge
		0x0b, // end loop
		0x0b, // end function
	)
	return modMem(t, 1, []wasm.ValType{wasm.I32}, nil, body)
}

func TestRepeatedStoreRegionGuardedExecution(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := repeatedStoreRegionModule(t)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		ElideBoundsChecks: true,
		Stats:             &stats,
		Optimizations:     map[string]bool{"repeated-store-region": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	cm.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["repeated-store-region"]; got == 0 {
		t.Fatal("guarded repeated store region was not folded")
	}

	initial := bytes.Repeat([]byte{0xff}, 65536)
	for _, address := range []uint64{0, 65472, 65500, 65535, 0xffffffff} {
		var want []byte
		var wantTrap bool
		for _, enabled := range []bool{false, true} {
			got, err := runLinearSpanMemory(t, m, CompileOptions{
				ElideBoundsChecks: true,
				Optimizations:     map[string]bool{"repeated-store-region": enabled},
			}, initial, address)
			if !enabled {
				want, wantTrap = got, err != nil
				continue
			}
			if (err != nil) != wantTrap || !bytes.Equal(got, want) {
				t.Fatalf("address=%#x trap=%v wantTrap=%v memoryEqual=%v", address, err, wantTrap, bytes.Equal(got, want))
			}
		}
	}
}
