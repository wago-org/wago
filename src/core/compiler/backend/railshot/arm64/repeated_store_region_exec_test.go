//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

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
