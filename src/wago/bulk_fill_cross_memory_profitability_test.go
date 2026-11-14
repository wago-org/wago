//go:build ((linux && amd64) || ((linux || darwin || windows) && arm64)) && !tinygo

package wago

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBulkFillCopyAcrossMemoryTrapPrefix(t *testing.T) {
	if !SupportedFeatures().IsEnabled(CoreFeatureMultiMemory) {
		t.Skip("multi-memory unavailable")
	}
	for _, source := range []byte{0, 1} {
		destination := 1 - source
		body := []byte{0x20, 1, 0x41, 7, 0x20, 2, 0xfc, 11, source, 0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, destination, source, 0x0b}
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1}, []byte{1, 1, 1})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0), wasmtest.ExportEntry("source", 2, uint32(source)), wasmtest.ExportEntry("destination", 2, uint32(destination)))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureMultiMemory).WithBoundsChecks(BoundsChecksExplicit).Compile(module)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		src, err := in.ExportedMemory("source")
		if err != nil {
			t.Fatal(err)
		}
		dst, err := in.ExportedMemory("destination")
		if err != nil {
			t.Fatal(err)
		}
		beforeSrc, beforeDst := make([]byte, 65536), make([]byte, 65536)
		for i := range beforeSrc {
			beforeSrc[i] = byte(i*29 + i>>8)
			beforeDst[i] = byte(i*11 + 17)
		}
		for _, n := range []uint32{0, 1, 15, 16, 63, 64, 65, 4096, 65536} {
			for _, pair := range [][2]uint32{{0, 0}, {129, 256}, {65536 - n, 0}, {0, 65536 - n}, {65537 - n, 0}, {0, 65537 - n}, {65536, 65536}, {0, 0xfffffff0}} {
				copy(src.UnsafeBytes(), beforeSrc)
				copy(dst.UnsafeBytes(), beforeDst)
				wantSrc, wantDst := bytes.Clone(beforeSrc), bytes.Clone(beforeDst)
				validFill := uint64(pair[1])+uint64(n) <= 65536
				validCopy := validFill && uint64(pair[0])+uint64(n) <= 65536
				if validFill {
					for i := uint64(pair[1]); i < uint64(pair[1])+uint64(n); i++ {
						wantSrc[i] = 7
					}
				}
				if validCopy {
					copy(wantDst[pair[0]:uint64(pair[0])+uint64(n)], wantSrc[pair[1]:uint64(pair[1])+uint64(n)])
				}
				_, err = in.Invoke("copy", uint64(pair[0])|0xaabbccdd00000000, uint64(pair[1])|0xaabbccdd00000000, uint64(n)|0xaabbccdd00000000)
				var trap *TrapError
				if validCopy && err != nil || !validCopy && (!errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds) || !bytes.Equal(src.UnsafeBytes(), wantSrc) || !bytes.Equal(dst.UnsafeBytes(), wantDst) {
					t.Fatalf("source%d dst%x src%x n%d valid%t: %v", source, pair[0], pair[1], n, validCopy, err)
				}
			}
		}
	}
}
