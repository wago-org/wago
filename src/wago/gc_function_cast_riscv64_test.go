//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled && !wago_guardpage

package wago

import (
	"fmt"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func rv64CollectorFunctionCastSource(graph string, vector bool) []byte {
	types := [][]byte{{0x5f, 1, 0x7f, 0}, {0x60, 0, 1, 0x7f}, {0x60, 0, 1, 0x7e}}
	targetType := byte(1)
	if graph == "subtype" {
		types[1] = []byte{0x50, 0, 0x60, 0, 1, 0x7f}
		types = append(types, []byte{0x4f, 1, 1, 0x60, 0, 1, 0x7f})
		targetType = 3
	}
	if graph == "recursive" {
		types[1] = []byte{0x4e, 2, 0x60, 0, 1, 0x7f, 0x60, 0, 1, 0x7e}
		types = types[:2]
	}
	functions := [][]byte{{targetType}, {2}, {1}, {1}, {1}, {1}}
	bodies := [][]byte{wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code([]byte{0x42, 7, 0x0b})}
	for _, op := range []byte{20, 20, 22, 22} {
		target := byte(0)
		if len(bodies) == 3 || len(bodies) == 5 {
			target = 1
		}
		body := []byte{0x41, 1, 0xfb, 0, 0, 0x1a}
		if vector {
			body = append(body, 0xfd, 12)
			body = append(body, make([]byte, 16)...)
			body = append(body, 0x1a)
		}
		body = append(body, 0xd2, target, 0xfb, op, 1)
		if op == 22 {
			body = append(body, 0x1a, 0x41, 1)
		}
		bodies = append(bodies, wasmtest.Code(append(body, 0x0b)))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(functions...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("test", 0, 2), wasmtest.ExportEntry("unrelated", 0, 3), wasmtest.ExportEntry("cast", 0, 4), wasmtest.ExportEntry("cast-unrelated", 0, 5))),
		wasmtest.Section(9, []byte{1, 3, 0, 2, 0, 1}), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

func TestRV64CollectorFunctionTestsAndCasts(t *testing.T) {
	for _, graph := range []string{"flat", "subtype", "recursive"} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/vector%t", graph, vector), func(t *testing.T) {
				cfg := rv64RootedScalarConfig()
				c, err := cfg.Compile(rv64CollectorFunctionCastSource(graph, vector))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				blob, err := c.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := LoadTrustedArtifact(blob)
				if err != nil {
					t.Fatal(err)
				}
				defer loaded.Close()
				for _, code := range []*Compiled{c, loaded} {
					in, err := instantiateCore(code, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					for _, name := range []string{"test", "unrelated", "cast", "cast-unrelated"} {
						got, err := in.Invoke(name)
						if name == "cast-unrelated" {
							if err == nil {
								t.Fatalf("unrelated cast returned %v", got)
							}
							continue
						}
						want := uint64(1)
						if name == "unrelated" {
							want = 0
						}
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("%s=%v,%v; want %d", name, got, err, want)
						}
					}
				}
			})
		}
	}
}
