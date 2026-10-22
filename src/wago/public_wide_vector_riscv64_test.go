//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64WideVectorCallModule(count int, tail bool) []byte {
	values := make([]wasm.ValType, count)
	var identity []byte
	for i := range values {
		values[i] = wasm.V128
		identity = append(identity, 0x20)
		identity = append(identity, wasmtest.ULEB(uint32(i))...)
	}
	opcode := byte(0x10)
	if tail {
		opcode = 0x12
	}
	caller := append(slices.Clone(identity), opcode, 1, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(values, values))),
		wasmtest.Section(3, []byte{2, 0, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), wasmtest.Code(append(identity, 0x0b)))),
	)
}

func rv64WideVectorCallCompile(t *testing.T, count int, tail, artifact bool) *Compiled {
	t.Helper()
	mode, dir := os.Getenv("WAGO_RV64_WIDE_CALL_ARTIFACT_MODE"), os.Getenv("WAGO_RV64_WIDE_CALL_ARTIFACT_DIR")
	name := filepath.Join(dir, fmt.Sprintf("wide-vector-%d-tail%t.wago", count, tail))
	if mode == "load" {
		blob, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		c, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	c, err := Compile(nil, rv64WideVectorCallModule(count, tail))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if artifact || mode == "prepare" {
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if mode == "prepare" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, blob, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if artifact {
			loaded, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { loaded.Close() })
			return loaded
		}
	}
	return c
}

func TestRV64PublicWideVectorLocalCalls(t *testing.T) {
	for _, count := range []int{1, 129, 256} {
		for _, tail := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				if os.Getenv("WAGO_RV64_WIDE_CALL_ARTIFACT_MODE") == "load" && !artifact {
					continue
				}
				t.Run(fmt.Sprintf("vectors%d-tail%t-artifact%t", count, tail, artifact), func(t *testing.T) {
					c := rv64WideVectorCallCompile(t, count, tail, artifact)
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					args := make([]uint64, 2*count)
					for i := range args {
						args[i] = uint64(i)*0x9e3779b97f4a7c15 ^ 0x7ff0000000000001
					}
					if got, err := in.Invoke("run", args...); err != nil || !slices.Equal(got, args) {
						t.Fatalf("wide vector identity: slots=%d error=%v equal=%t", len(got), err, slices.Equal(got, args))
					}
					allocations := testing.AllocsPerRun(20, func() {
						got, err := in.Invoke("run", args...)
						if err != nil || !slices.Equal(got, args) {
							panic("wide vector identity changed on warm entry")
						}
					})
					t.Logf("warm call allocations=%g; physical slots=%d", allocations, len(args))
				})
			}
		}
	}
}
